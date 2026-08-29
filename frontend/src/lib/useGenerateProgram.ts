import { useEffect, useRef, useState } from "react";
import { getLatestProgram, runBatch } from "./api";
import { useUserId } from "./useUserId";
import type { Program } from "./types";

const POLL_INTERVAL_MS = 5000;
const MAX_POLLS = 24; // 5秒間隔で最大2分待つ

// 開発・デモ用の手動生成ロジック。POST /api/batch/runは全ユーザー分をまとめて
// 生成する非同期エンドポイントのため、起動後にgetLatestProgramをポーリングして
// 自分の分の完了を待つ。Home（準備中の手動生成ボタン）とOnboarding（初回登録後の
// 生成中画面）の両方から使うため共通化している。
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

  async function generate(onReady: (program: Program) => void) {
    if (!userId || generating) return;
    setGenerating(true);
    setError(null);

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
