import { useEffect, useRef, useState } from "react";
import { getLatestProgram, regenerateLatestProgram, runBatch } from "./api";
import { useUserId } from "./useUserId";
import type { Program } from "./types";

const POLL_INTERVAL_MS = 5000;
const MAX_POLLS = 24; // 5秒間隔で最大2分待つ

// DEMO_MODEのregenerateはDBから既存番組をランダムに引いて返すだけなので一瞬で
// 終わる。Onboardingの「生成中…」演出（GENERATING_STATUS_MESSAGES、最後の切り替え
// が1800ms時点）が一瞬でスキップされてしまわないよう、regenerateモードには最低
// この時間だけ「生成中」状態を維持する下駄を履かせる（実際の生成が遅い本番運用時は
// 素通りになるだけで実害はない）。
const MIN_REGENERATE_DISPLAY_MS = 2800;

export type GenerateMode = "batch" | "regenerate";

// 開発・デモ用の手動生成ロジック。デフォルト（mode: "batch"）はPOST /api/batch/run
// を使う。全ユーザー分をまとめて生成する非同期エンドポイントのため、起動後に
// getLatestProgramをポーリングして自分の分の完了を待つ。Home（準備中の手動生成
// ボタン）から使う。
//
// mode: "regenerate" はOnboarding（初回登録直後の生成）専用。batch/runは配信時刻
// を過ぎていて今日の分が未生成の他ユーザーまで巻き込んで生成してしまうため、
// 自分のuserIdだけを対象にするPOST .../regenerateを使う。regenerateは同期的に
// 完成した番組を返すのでポーリングは不要。resetCountを消費しないよう
// bypassResetLimit付きで呼ぶ（初回生成が3回/日の作り直し枠を食いつぶさないため）。
export function useGenerateProgram() {
  const { userId } = useUserId();
  const [generating, setGenerating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const cancelledRef = useRef(false);

  useEffect(() => {
    cancelledRef.current = false;
    return () => {
      cancelledRef.current = true;
    };
  }, []);

  async function generate(onReady: (program: Program) => void, mode: GenerateMode = "batch") {
    if (!userId || generating) return;
    setGenerating(true);
    setError(null);

    if (mode === "regenerate") {
      const startedAt = Date.now();
      try {
        const program = await regenerateLatestProgram(userId, { bypassResetLimit: true });
        const elapsedMs = Date.now() - startedAt;
        if (elapsedMs < MIN_REGENERATE_DISPLAY_MS) {
          await new Promise((resolve) => setTimeout(resolve, MIN_REGENERATE_DISPLAY_MS - elapsedMs));
        }
        if (!cancelledRef.current) {
          setGenerating(false);
          onReady(program);
        }
      } catch {
        if (!cancelledRef.current) {
          setGenerating(false);
          setError("生成に失敗しました。もう一度お試しください。");
        }
      }
      return;
    }

    try {
      await runBatch();
    } catch {
      if (!cancelledRef.current) {
        setGenerating(false);
        setError("生成の開始に失敗しました。もう一度お試しください。");
      }
      return;
    }

    for (let i = 0; i < MAX_POLLS; i++) {
      await new Promise((resolve) => setTimeout(resolve, POLL_INTERVAL_MS));
      if (cancelledRef.current) return;
      try {
        const program = await getLatestProgram(userId);
        if (cancelledRef.current) return;
        setGenerating(false);
        onReady(program);
        return;
      } catch {
        // まだ生成中の可能性があるためポーリングを続ける
      }
    }

    if (!cancelledRef.current) {
      setGenerating(false);
      setError("生成に時間がかかっています。しばらくしてからこの画面を開き直してください。");
    }
  }

  return { generating, error, generate };
}
