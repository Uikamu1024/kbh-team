import { useEffect, useRef, useState } from "react";
import { ApiError, getGenerationStatus, getLatestProgram, regenerateLatestProgram } from "./api";
import { useUserId } from "./useUserId";
import type { ApiErrorCode, GenerationStage, Program } from "./types";

const POLL_INTERVAL_MS = 1500;

// 番組の手動生成ロジック。POST /api/users/{userId}/programs/latest/regenerate
// は202 Acceptedを即座に返す非同期APIで、実際のパイプライン（記事取得〜台本
// 生成〜音声合成〜保存）はサーバー側のバックグラウンドで進む——このリクエスト
// を送ったブラウザのタブを閉じたりページをリロードしたりしても、生成処理
// 自体は止まらない。そのため完了/失敗の判定は最初のPOSTのレスポンスではなく
// GET .../generation-statusのポーリングだけで行う。この設計により、画面を
// 開き直したとき（別画面から戻ってきた、あるいはページをリロードした）でも
// マウント時に一度状態を確認するだけで、進行中の生成を正しく引き継いで表示できる。
export function useGenerateProgram() {
  const { userId } = useUserId();
  const [generating, setGenerating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [stage, setStage] = useState<GenerationStage | null>(null);
  const cancelledRef = useRef(false);
  const pollIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const onReadyRef = useRef<((program: Program) => void) | null>(null);

  function stopPolling() {
    if (pollIntervalRef.current !== null) {
      clearInterval(pollIntervalRef.current);
      pollIntervalRef.current = null;
    }
  }

  function startPolling(uid: string) {
    if (pollIntervalRef.current !== null) return;
    pollIntervalRef.current = setInterval(() => {
      if (cancelledRef.current) return;
      getGenerationStatus(uid)
        .then(async (status) => {
          if (cancelledRef.current) return;
          if (status.generating) {
            if (status.stage) setStage(status.stage);
            return;
          }

          // 生成が終わっていた（成功・失敗どちらか）。完了検知はここだけで行う
          // （最初のPOSTは202 Acceptedを返すだけで結果を持っていないため）。
          stopPolling();
          setGenerating(false);
          const onReady = onReadyRef.current;
          onReadyRef.current = null;

          if (status.lastError) {
            setError(errorMessageForCode(status.lastError.code));
            return;
          }
          if (!onReady) return; // このマウントが開始した生成ではない（引き継いで表示していただけ）
          try {
            const program = await getLatestProgram(uid);
            if (cancelledRef.current) return;
            onReady(program);
          } catch {
            if (cancelledRef.current) return;
            setError("生成した番組の取得に失敗しました。もう一度お試しください。");
          }
        })
        .catch(() => {
          // ポーリング失敗は無視して次回再試行する
        });
    }, POLL_INTERVAL_MS);
  }

  // マウント時（画面を開き直した／別画面から戻ってきた／ページをリロードした
  // とき）：既にこのユーザーの生成がサーバー側で進行中なら、その状態を引き継いで
  // 表示する（ポーリングも再開する）。
  useEffect(() => {
    cancelledRef.current = false;
    if (userId) {
      getGenerationStatus(userId)
        .then((status) => {
          if (cancelledRef.current || !status.generating) return;
          setGenerating(true);
          if (status.stage) setStage(status.stage);
          startPolling(userId);
        })
        .catch(() => {
          // 起動直後でバックエンドに繋がらない等は無視し、通常のidle表示のままにする
        });
    }
    return () => {
      cancelledRef.current = true;
      stopPolling();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [userId]);

  async function generate(onReady: (program: Program) => void) {
    if (!userId || generating) return;
    setGenerating(true);
    setError(null);
    setStage(null);
    onReadyRef.current = onReady;

    try {
      await regenerateLatestProgram(userId);
      // 202 Accepted：まだ完了していない。完了の検知はポーリング側に任せる
      if (cancelledRef.current) return;
      startPolling(userId);
    } catch (err) {
      if (cancelledRef.current) return;
      setGenerating(false);
      onReadyRef.current = null;
      if (err instanceof ApiError && err.code === "ALREADY_GENERATING") {
        setError("前回のリクエストを処理中です。しばらく待ってから再度お試しください。");
      } else {
        setError("番組の生成を開始できませんでした。もう一度お試しください。");
      }
    }
  }

  return { generating, error, stage, generate };
}

function errorMessageForCode(code: ApiErrorCode): string {
  switch (code) {
    case "ARTICLE_CACHE_EMPTY":
      return "選んだテーマの記事が見つかりませんでした。テーマを変えて再度お試しください。";
    case "RESET_LIMIT_EXCEEDED":
      return "本日の上限に達しています。";
    case "NO_UNSEEN_ARTICLES":
      return "選んだテーマの新着記事をまだ配信し終えています。時間をおくか、テーマを変えてお試しください。";
    default:
      return "番組の生成に失敗しました。もう一度お試しください。";
  }
}
