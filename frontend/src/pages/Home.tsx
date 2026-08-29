import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ApiError, getLatestProgram, listPrograms, runBatch } from "@/lib/api";
import { useUserId } from "@/lib/useUserId";
import { getDisplayName } from "@/lib/user";
import { formatDateLabel, formatMinutesLabel } from "@/lib/format";
import type { Program, ProgramSummary } from "@/lib/types";

type LoadState =
  | { status: "loading" }
  | { status: "not-ready" }
  | { status: "error" }
  | { status: "ready"; program: Program };

export default function Home() {
  const navigate = useNavigate();
  const { userId } = useUserId();
  const [result, setResult] = useState<{ key: string; state: LoadState } | null>(null);
  const [history, setHistory] = useState<ProgramSummary[]>([]);
  const [attempt, setAttempt] = useState(0);
  const [generating, setGenerating] = useState(false);
  const [generateError, setGenerateError] = useState<string | null>(null);
  const cancelledRef = useRef(false);
  const requestKey = `${userId}:${attempt}`;

  useEffect(() => {
    cancelledRef.current = false;
    return () => {
      cancelledRef.current = true;
    };
  }, []);

  useEffect(() => {
    if (!userId) return;
    let cancelled = false;
    const key = `${userId}:${attempt}`;

    getLatestProgram(userId)
      .then((program) => {
        if (!cancelled) setResult({ key, state: { status: "ready", program } });
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.code === "PROGRAM_NOT_FOUND") {
          setResult({ key, state: { status: "not-ready" } });
        } else if (err instanceof ApiError && err.code === "USER_NOT_FOUND") {
          // DBリセット等でuserIdが存在しなくなっている。入口（RootGate）の
          // 自動復旧に任せる（古いIDを破棄して新規発行→オンボーディングへ）。
          navigate("/", { replace: true });
        } else {
          setResult({ key, state: { status: "error" } });
        }
      });

    listPrograms(userId, 10)
      .then((res) => {
        if (!cancelled) setHistory(res.items);
      })
      .catch(() => {
        // 履歴取得の失敗はホーム画面全体をブロックしない
      });

    return () => {
      cancelled = true;
    };
  }, [userId, attempt, navigate]);

  const state: LoadState = result?.key === requestKey ? result.state : { status: "loading" };

  // 開発・デモ用の手動生成。batch/runは全ユーザー分をまとめて生成する非同期エンドポイント
  // のため、起動後にgetLatestProgramをポーリングして自分の分の完了を待つ。
  async function handleGenerateNow() {
    if (!userId || generating) return;
    setGenerating(true);
    setGenerateError(null);

    try {
      await runBatch();
    } catch {
      if (!cancelledRef.current) {
        setGenerating(false);
        setGenerateError("生成の開始に失敗しました。もう一度お試しください。");
      }
      return;
    }

    const key = `${userId}:${attempt}`;
    const maxPolls = 24; // 5秒間隔で最大2分待つ
    for (let i = 0; i < maxPolls; i++) {
      await new Promise((resolve) => setTimeout(resolve, 5000));
      if (cancelledRef.current) return;
      try {
        const program = await getLatestProgram(userId);
        if (cancelledRef.current) return;
        setResult({ key, state: { status: "ready", program } });
        setGenerating(false);
        return;
      } catch {
        // まだ生成中の可能性があるためポーリングを続ける
      }
    }

    if (!cancelledRef.current) {
      setGenerating(false);
      setGenerateError(
        "生成に時間がかかっています。しばらくしてからこの画面を開き直してください。",
      );
    }
  }

  if (state.status === "loading") {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-text-tertiary">読み込んでいます…</p>
      </div>
    );
  }

  if (state.status === "error") {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 text-center">
        <p className="text-sm text-text-secondary">
          番組の取得に失敗しました。backendが起動しているか確認してください。
        </p>
        <button
          type="button"
          onClick={() => setAttempt((n) => n + 1)}
          className="rounded-full bg-accent px-5 py-2 text-sm font-semibold text-[#06120a]"
        >
          再試行
        </button>
      </div>
    );
  }

  if (state.status === "not-ready") {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 text-center">
        <p className="text-[15px] font-semibold">今日の番組を準備しています</p>
        <p className="text-sm text-text-tertiary">
          毎朝6:00ごろに配信されます。少し待ってからもう一度開いてください。
        </p>
        <button
          type="button"
          disabled={generating}
          onClick={handleGenerateNow}
          className="mt-4 rounded-full bg-accent px-5 py-2 text-sm font-semibold text-[#06120a] transition-opacity disabled:cursor-not-allowed disabled:opacity-50"
        >
          {generating ? "生成しています…" : "今すぐ生成する"}
        </button>
        {generating && (
          <p className="text-xs text-text-tertiary">数十秒〜1分ほどかかります</p>
        )}
        {generateError && <p className="text-sm text-danger">{generateError}</p>}
      </div>
    );
  }

  const { program } = state;

  return (
    <div className="pt-4">
      <section className="mt-2">
        <p className="text-[19px] font-semibold leading-relaxed">
          {getDisplayName()}さん、おはようございます。
          <br />
          今日は<strong className="text-[21px] text-accent">{program.changeCount}</strong>
          件、動きがあります。
        </p>
      </section>

      <section className="mt-8 flex flex-col items-center">
        <button
          type="button"
          aria-label="今日の番組を再生"
          onClick={() => navigate("/player?autoplay=1")}
          className="flex flex-col items-center justify-center gap-1.5 rounded-full text-[#06120a] shadow-[0_0_0_10px_rgba(34,197,94,0.08),0_12px_34px_var(--accent-glow)] transition-transform hover:scale-[1.03] active:scale-[0.97]"
          style={{
            width: 168,
            height: 168,
            background:
              "radial-gradient(circle at 32% 28%, #6ee7a8, var(--accent) 60%, #12813f 100%)",
          }}
        >
          <svg viewBox="0 0 24 24" width="34" height="34" fill="currentColor">
            <path d="M8 5v14l11-7z" />
          </svg>
          <span className="text-[15px] font-bold">再生</span>
        </button>
        <p className="mt-3.5 text-[13px] text-text-secondary">
          今日の番組・{program.chapters.length}チャプター・
          {formatMinutesLabel(program.totalDurationSec)}
        </p>
      </section>

      <section className="mt-6">
        <h2 className="mb-3 mt-5 text-sm font-bold">履歴</h2>
        <ul className="flex flex-col gap-2.5">
          {history.length === 0 && (
            <li className="text-sm text-text-tertiary">まだ履歴がありません</li>
          )}
          {history
            .filter((item) => item.id !== program.id)
            .map((item) => (
              <li key={item.id}>
                <button
                  type="button"
                  onClick={() => navigate(`/player?programId=${item.id}`)}
                  className="flex w-full items-center gap-3 rounded-2xl bg-bg-elevated px-3 py-2.5 text-left transition-colors hover:bg-bg-elevated-2"
                >
                  <div className="flex h-[46px] w-[46px] shrink-0 items-center justify-center rounded-lg bg-gradient-to-br from-bg-elevated-3 to-bg-elevated-2 text-text-tertiary">
                    <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2">
                      <path d="M9 18V5l12-2v13" />
                      <circle cx="6" cy="18" r="3" />
                      <circle cx="18" cy="16" r="3" />
                    </svg>
                  </div>
                  <div className="min-w-0 flex-1">
                    <p className="m-0 text-[11px] text-text-tertiary">
                      {formatDateLabel(item.createdAt)}
                    </p>
                    <p className="m-0 truncate text-sm font-semibold">{item.title}</p>
                  </div>
                  <span className="shrink-0 text-xs text-text-tertiary">
                    {formatMinutesLabel(item.totalDurationSec)}
                  </span>
                </button>
              </li>
            ))}
        </ul>
      </section>
    </div>
  );
}
