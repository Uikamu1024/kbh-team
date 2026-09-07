import { useEffect, useRef, useState } from "react";
import { ApiError, getGenerationStatus, getLatestProgram, regenerateLatestProgram } from "./api";
import { useUserId } from "./useUserId";
import type { ApiErrorCode, GenerationStage, Program, RegenerateAcceptedResponse } from "./types";

// regenerateLatestProgramは202（RegenerateAcceptedResponse）と201（Program、
// DEMO_MODE時のみ）のどちらかを返すため、レスポンスの形からどちらだったかを
// 判別する（Programには必ずchaptersが含まれるため、これが無ければacceptedの方）。
function isRegenerateAccepted(
  response: RegenerateAcceptedResponse | Program,
): response is RegenerateAcceptedResponse {
  return !("chapters" in response);
}

const POLL_INTERVAL_MS = 1500;

// DEMO_MODE時、regenerateはDBから既存番組をランダムに引いて同期的に返すだけ
// なので一瞬で終わる。Onboardingの「生成中…」演出（GENERATING_STATUS_MESSAGES、
// 最後の切り替えが1800ms時点）が一瞬でスキップされてしまわないよう、同期応答
// だった場合は最低この時間だけ「生成中」状態を維持する下駄を履かせる（実際の
// 生成が遅い非DEMO_MODE運用時は後述のポーリングが自然にこれより長くかかるため
// 素通りになるだけで実害はない）。
const MIN_SYNC_DISPLAY_MS = 2800;

// 番組の手動生成ロジック。POST /api/users/{userId}/programs/latest/regenerate
// はDEMO_MODE時のみDBから既存番組を割り当てて201 + Programを同期的に返すが、
// 通常運用時は202 Acceptedを即座に返す非同期APIで、実際のパイプライン（記事
// 取得〜台本生成〜音声合成〜保存）はサーバー側のバックグラウンドで進む——この
// リクエストを送ったブラウザのタブを閉じたりページをリロードしたりしても、
// 生成処理自体は止まらない。レスポンスが同期的なProgramかどうかは呼び出し前
// には分からないため、`generate`はレスポンスの形で判別し、202だった場合だけ
// GET .../generation-statusのポーリングに切り替える。この設計により、画面を
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

  // options.bypassResetLimit：オンボーディング直後の初回生成・ホームの「今すぐ
  // 生成する」で使う。その日の最初の1本は「作り直し」ではないため、1日3回までの
  // リセット上限にカウントしない（Profile画面の明示的な「作り直す」はこれを
  // 付けずに呼び、上限を消費する）。
  async function generate(
    onReady: (program: Program) => void,
    options?: { bypassResetLimit?: boolean },
  ) {
    if (!userId || generating) return;
    setGenerating(true);
    setError(null);
    setStage(null);
    onReadyRef.current = onReady;

    const startedAt = Date.now();
    try {
      const response = await regenerateLatestProgram(userId, options);
      if (cancelledRef.current) return;

      if (isRegenerateAccepted(response)) {
        // 202 Accepted：まだ完了していない。完了の検知はポーリング側に任せる
        startPolling(userId);
        return;
      }

      // DEMO_MODE：201 + Programが同期的に返ってきた
      const elapsedMs = Date.now() - startedAt;
      if (elapsedMs < MIN_SYNC_DISPLAY_MS) {
        await new Promise((resolve) => setTimeout(resolve, MIN_SYNC_DISPLAY_MS - elapsedMs));
      }
      if (cancelledRef.current) return;
      setGenerating(false);
      onReadyRef.current = null;
      onReady(response);
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
