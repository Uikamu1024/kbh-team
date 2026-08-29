import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ApiError, getLatestProgram, getUser, listPrograms } from "@/lib/api";
import { useUserId } from "@/lib/useUserId";
import { getDisplayName } from "@/lib/user";
import { usePlayback } from "@/lib/PlaybackContext";
import { useGenerateProgram } from "@/lib/useGenerateProgram";
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
  const playback = usePlayback();
  const [result, setResult] = useState<{ key: string; state: LoadState } | null>(null);
  const [history, setHistory] = useState<ProgramSummary[]>([]);
  const [tags, setTags] = useState<string[]>([]);
  const [attempt, setAttempt] = useState(0);
  const { generating, error: generateError, generate } = useGenerateProgram();
  const requestKey = `${userId}:${attempt}`;

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

    getUser(userId)
      .then((profile) => {
        if (!cancelled) setTags(profile.tags);
      })
      .catch(() => {
        // タグ表示の失敗もホーム画面全体をブロックしない
      });

    return () => {
      cancelled = true;
    };
  }, [userId, attempt, navigate]);

  const state: LoadState = result?.key === requestKey ? result.state : { status: "loading" };

  function handleGenerateNow() {
    const key = `${userId}:${attempt}`;
    generate((program) => setResult({ key, state: { status: "ready", program } }));
  }

  // 今日の番組がすでに再生中ならその場でトグル、そうでなければ読み込んで
  // プレイヤー画面へ遷移する（mock/main/app.js のheroPlayBtnの挙動に合わせる）。
  function handleHeroClick() {
    const isTodayLoaded = playback.source?.type === "latest";
    if (isTodayLoaded && playback.isPlaying) {
      playback.pause();
      return;
    }
    if (!(isTodayLoaded && playback.status === "ready")) {
      playback.loadLatest({ autoplay: true });
    } else {
      playback.play();
    }
    navigate("/player");
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
          className="rounded-full bg-accent px-5 py-2 text-sm font-semibold text-white"
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
          className="mt-4 rounded-full bg-accent px-5 py-2 text-sm font-semibold text-white transition-opacity disabled:cursor-not-allowed disabled:opacity-50"
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
  const heroPlaying = playback.source?.type === "latest" && playback.isPlaying;

  return (
    <div className="pt-4">
      <section className="mt-2">
        <p className="text-[22px] font-bold leading-relaxed tracking-tight">
          {getDisplayName()}さん、おはようございます。
          <br />
          今日は<strong className="text-accent">{program.changeCount}</strong>
          件、動きがあります。
        </p>
      </section>

      <section className="mt-4.5 rounded-xl border border-bg-elevated-3 bg-bg-elevated p-3.5 shadow-sm"
        style={{ borderLeft: "4px solid var(--accent)" }}
      >
        <strong className="block text-text-primary">今日の要点</strong>
        <span className="mt-1 block text-[13px] leading-relaxed text-text-secondary">
          関心のあるテーマから、新しい動きを短くまとめています。
        </span>
      </section>

      <section className="my-4.5 flex flex-col items-center gap-4 border-y border-bg-elevated-3 py-4.5">
        <button
          type="button"
          aria-label={heroPlaying ? "今日の番組を一時停止" : "今日の番組を再生"}
          onClick={handleHeroClick}
          className="flex h-[124px] w-[124px] items-center justify-center rounded-full text-white shadow-md transition-transform hover:-translate-y-0.5 active:scale-[0.97]"
          style={{ background: heroPlaying ? "var(--signal)" : "var(--accent)" }}
        >
          {heroPlaying ? (
            <svg viewBox="0 0 24 24" width="42" height="42" fill="currentColor">
              <path d="M7 5h4v14H7zM13 5h4v14h-4z" />
            </svg>
          ) : (
            <svg viewBox="0 0 24 24" width="42" height="42" fill="currentColor">
              <path d="M8 5v14l11-7z" />
            </svg>
          )}
        </button>
        <p className="m-0 text-center text-xs text-text-secondary">
          今日の番組・{program.chapters.length}チャプター・
          {formatMinutesLabel(program.totalDurationSec)}
        </p>
      </section>

      {tags.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {tags.map((tag) => (
            <span
              key={tag}
              className="rounded-full bg-accent-soft px-2.5 py-1.5 text-[11px] text-accent"
            >
              {tag}
            </span>
          ))}
        </div>
      )}

      <section className="mt-6">
        <h2 className="mb-2.5 mt-7.5 text-sm font-extrabold">履歴</h2>
        <ul className="flex flex-col gap-0 border-t border-bg-elevated-3">
          {history.length === 0 && (
            <li className="py-2.5 text-sm text-text-tertiary">まだ履歴がありません</li>
          )}
          {history
            .filter((item) => item.id !== program.id)
            .map((item) => (
              <li key={item.id} className="border-b border-bg-elevated-3">
                <button
                  type="button"
                  onClick={() => navigate(`/player?programId=${item.id}`)}
                  className="flex w-full items-center gap-3 py-2.5 text-left transition-colors hover:bg-bg-elevated-2"
                >
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-[10px] bg-bg-elevated-2 text-signal">
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
