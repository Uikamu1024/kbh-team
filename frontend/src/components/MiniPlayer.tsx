import { useLocation, useNavigate } from "react-router-dom";
import { usePlayback } from "@/lib/PlaybackContext";

// 再生中に、フルプレイヤー以外の画面（ホーム・プロフィール）下部に浮かべる
// 小さい再生バー。タップでプレイヤー画面へ遷移する。
export function MiniPlayer() {
  const playback = usePlayback();
  const navigate = useNavigate();
  const location = useLocation();

  if (playback.status !== "ready" || !playback.program) return null;
  if (location.pathname.startsWith("/player")) return null;

  const chapter = playback.program.chapters[playback.chapterIndex];
  if (!chapter) return null;

  return (
    <aside
      className="fixed inset-x-3 z-10 flex items-center gap-2 rounded-xl border border-bg-elevated-3 bg-bg-elevated/95 p-2 shadow-lg backdrop-blur-md"
      style={{ bottom: "calc(64px + max(8px, env(safe-area-inset-bottom)) + 10px)" }}
    >
      <button
        type="button"
        onClick={() => navigate("/player")}
        className="flex min-w-0 flex-1 items-center gap-2.5 border-0 bg-transparent p-0 text-left"
      >
        <span
          className="flex h-[38px] w-[38px] shrink-0 items-center justify-center rounded-lg"
          style={{ background: "linear-gradient(160deg, #1c4d33, #0a1a12)" }}
          aria-hidden="true"
        >
          <span className="h-3.5 w-3.5 rounded-full border-2 border-white/85" />
        </span>
        <span className="flex min-w-0 flex-col gap-0.5">
          <strong className="truncate text-xs font-semibold text-text-primary">
            {chapter.title}
          </strong>
          <span className="text-[10px] text-text-secondary">
            {playback.chapterIndex + 1} / {playback.program.chapters.length}
          </span>
        </span>
      </button>
      <button
        type="button"
        aria-label={playback.isPlaying ? "一時停止" : "再生"}
        onClick={(e) => {
          e.stopPropagation();
          playback.toggle();
        }}
        className="flex h-[42px] w-[42px] shrink-0 items-center justify-center rounded-full bg-signal text-white"
      >
        {playback.isPlaying ? (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
            <path d="M7 5h4v14H7zM13 5h4v14h-4z" />
          </svg>
        ) : (
          <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
            <path d="M8 5v14l11-7z" />
          </svg>
        )}
      </button>
    </aside>
  );
}
