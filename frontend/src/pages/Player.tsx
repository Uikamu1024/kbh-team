import { useEffect, useMemo } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { usePlayback } from "@/lib/PlaybackContext";
import { formatSecondsAsClock } from "@/lib/format";

const WAVEFORM_BAR_COUNT = 48;

function buildWaveformHeights(seed: string): number[] {
  // 実際の波形データはAPIに存在しないため、見た目用の擬似波形を生成する
  // （docs/api-contract.yaml の x-open-questions 参照。装飾目的でバックエンドには影響しない）。
  let hash = 0;
  for (let i = 0; i < seed.length; i++) {
    hash = (hash * 31 + seed.charCodeAt(i)) >>> 0;
  }
  const heights: number[] = [];
  for (let i = 0; i < WAVEFORM_BAR_COUNT; i++) {
    const t = i / WAVEFORM_BAR_COUNT;
    const envelope = 0.35 + 0.65 * Math.sin(Math.PI * t) ** 0.6;
    hash = (hash * 1103515245 + 12345) >>> 0;
    const jitter = 0.55 + (hash % 1000) / 1000 / 2.2;
    heights.push(Math.max(4, Math.round(envelope * jitter * 32)));
  }
  return heights;
}

export default function Player() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const programIdParam = searchParams.get("programId");
  const autoplayParam = searchParams.get("autoplay") === "1";
  const playback = usePlayback();

  // 画面を開いたときだけ、必要ならソースを切り替える。すでに同じ番組が
  // 読み込み済み（例：ホームからの遷移で再生中）ならここでは何もせず、
  // 再生を途切れさせない。
  useEffect(() => {
    if (programIdParam) {
      const alreadyLoaded =
        playback.source?.type === "program" && playback.source.programId === programIdParam;
      if (!alreadyLoaded) {
        playback.loadProgram(programIdParam, { autoplay: autoplayParam });
      }
    } else if (!playback.source) {
      playback.loadLatest({ autoplay: autoplayParam });
    }
    // 初回マウント時のソース確定のみを目的とするため、programIdParam以外の
    // 変化では再実行しない（依存に含めるとplayback参照の変化のたびに
    // 再読み込みしてしまう）
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [programIdParam]);

  const { status, program, chapterIndex, chapterDurations, chapterElapsed, isPlaying } = playback;
  const chapter = program?.chapters[chapterIndex] ?? null;
  const lines = useMemo(
    () => (chapter ? chapter.script.split("\n").filter((line) => line.trim() !== "") : []),
    [chapter],
  );
  // 各行の開始位置（進捗比率0〜1）。チャプターには文単位のタイムスタンプが
  // 無いため（docs/api-contract.yamlのx-open-questions参照）、行の文字数に
  // 比例して読み上げ時間を按分する。均等割りだと長い行・短い行で実際の音声と
  // 数秒単位でずれるため、TTSの読み上げ速度がおおよそ文字数に比例するという
  // 前提（backend側のestimatedCharactersPerMinuteと同じ考え方）で近似する。
  const lineStartRatios = useMemo(() => {
    const totalChars = lines.reduce((sum, line) => sum + line.length, 0);
    if (totalChars === 0) return lines.map((_, i) => i / Math.max(1, lines.length));
    const ratios: number[] = [];
    let acc = 0;
    for (const line of lines) {
      ratios.push(acc / totalChars);
      acc += line.length;
    }
    return ratios;
  }, [lines]);
  const waveform = useMemo(
    () => (chapter ? buildWaveformHeights(chapter.id) : []),
    [chapter],
  );

  if (status === "loading") {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-text-tertiary">読み込んでいます…</p>
      </div>
    );
  }

  if (status === "not-ready") {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 text-center">
        <p className="text-[15px] font-semibold">まだ番組がありません</p>
        <p className="text-sm text-text-tertiary">
          今日の番組が準備できてから、もう一度開いてください。
        </p>
        <button
          type="button"
          onClick={() => navigate("/home")}
          className="rounded-full bg-accent px-5 py-2 text-sm font-semibold text-white"
        >
          ホームへ戻る
        </button>
      </div>
    );
  }

  if (status === "error" || !program || !chapter) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 text-center">
        <p className="text-sm text-text-secondary">番組の取得に失敗しました。</p>
        <button
          type="button"
          onClick={playback.retry}
          className="rounded-full bg-accent px-5 py-2 text-sm font-semibold text-white"
        >
          再試行
        </button>
      </div>
    );
  }

  const chapterDuration = chapterDurations[chapterIndex] ?? chapter.durationSec;
  const progressRatio = chapterDuration > 0 ? chapterElapsed / chapterDuration : 0;
  let activeLineIndex = 0;
  for (let i = 0; i < lineStartRatios.length; i++) {
    if (progressRatio >= lineStartRatios[i]) activeLineIndex = i;
  }
  const playedBars = Math.round(waveform.length * progressRatio);

  return (
    <div className="pt-2">
      <div
        className="relative flex min-h-[180px] overflow-hidden rounded-2xl p-6"
        style={{ background: "#6a9c86" }}
      >
        <div
          className="pointer-events-none absolute -inset-[40%] blur-[16px]"
          style={{
            background: "radial-gradient(circle at 76% 18%, rgba(255,255,255,.5), transparent 52%)",
          }}
        />
        <span className="pointer-events-none absolute bottom-[18px] right-5 text-[10px] tracking-[.14em] text-white/65">
          DAILY / {String(chapterIndex + 1).padStart(2, "0")}
        </span>
        <div className="relative z-[1] flex flex-col justify-end gap-2">
          {lines.map((line, i) => (
            <p
              key={i}
              className={`m-0 font-semibold leading-relaxed transition-all duration-300 ${
                i === activeLineIndex
                  ? "translate-x-0.5 text-[15px] text-white"
                  : "text-[13px] text-white/50"
              }`}
            >
              {line}
            </p>
          ))}
        </div>
      </div>

      <div className="mt-4.5">
        <a
          href={chapter.sourceUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="text-xs text-signal hover:underline"
        >
          {chapter.sourceName}
        </a>
        <h2 className="m-0 mt-1.5 text-[19px] font-extrabold leading-snug">{chapter.title}</h2>
        <p className="m-0 mt-1.5 text-xs text-text-secondary">
          {chapterIndex + 1} / {program.chapters.length}
        </p>
      </div>

      <div className="mt-4.5">
        <div
          className="flex h-[42px] cursor-pointer items-center gap-[3px] py-1.5"
          onClick={(e) => {
            const rect = e.currentTarget.getBoundingClientRect();
            playback.seek(Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width)));
          }}
        >
          {waveform.map((h, i) => (
            <div
              key={i}
              className={`min-w-[3px] flex-1 rounded-sm transition-colors ${
                i < playedBars ? "bg-signal" : "bg-bg-elevated-3"
              }`}
              style={{ height: h }}
            />
          ))}
        </div>
        <div className="mt-1 flex justify-between text-[11px] text-text-tertiary">
          <span>{formatSecondsAsClock(chapterElapsed)}</span>
          <span>{formatSecondsAsClock(chapter.durationSec)}</span>
        </div>
      </div>

      <div className="my-1.5 flex items-center justify-center gap-8">
        <button
          type="button"
          aria-label="前の記事"
          onClick={() => playback.goToChapter(chapterIndex - 1)}
          className="flex min-h-[46px] min-w-[46px] items-center justify-center text-text-primary transition-transform hover:opacity-80 active:scale-90"
        >
          <svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor">
            <path d="M6 6h2v12H6zm3.5 6l8.5 6V6z" />
          </svg>
        </button>
        <button
          type="button"
          aria-label={isPlaying ? "一時停止" : "再生"}
          onClick={playback.toggle}
          className="flex h-16 w-16 items-center justify-center rounded-full bg-text-primary text-bg shadow-md transition-transform hover:scale-[1.04] active:scale-95"
        >
          {isPlaying ? (
            <svg viewBox="0 0 24 24" width="26" height="26" fill="currentColor">
              <path d="M7 5h4v14H7zM13 5h4v14h-4z" />
            </svg>
          ) : (
            <svg viewBox="0 0 24 24" width="26" height="26" fill="currentColor">
              <path d="M8 5v14l11-7z" />
            </svg>
          )}
        </button>
        <button
          type="button"
          aria-label="次の記事"
          onClick={() => playback.goToChapter(chapterIndex + 1)}
          className="flex min-h-[46px] min-w-[46px] items-center justify-center text-text-primary transition-transform hover:opacity-80 active:scale-90"
        >
          <svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor">
            <path d="M16 6h2v12h-2zM6 6l8.5 6L6 18z" />
          </svg>
        </button>
      </div>

      <section>
        <h2 className="mb-2.5 mt-7.5 text-sm font-extrabold">この番組のチャプター</h2>
        <ul className="flex flex-col gap-0 border-t border-bg-elevated-3">
          {program.chapters.map((c, i) => (
            <li key={c.id} className="border-b border-bg-elevated-3">
              <button
                type="button"
                onClick={() => playback.goToChapter(i)}
                className={`flex w-full items-center gap-3 rounded-lg px-2 py-2.5 text-left transition-colors hover:bg-bg-elevated-2 ${
                  i === chapterIndex ? "border-l-4 border-signal bg-signal-soft pl-1.5" : ""
                }`}
              >
                {i === chapterIndex ? (
                  <span className="flex h-3.5 w-5 shrink-0 items-end justify-center gap-0.5">
                    <span className="eq-bar h-2/5 w-[3px] bg-signal" />
                    <span className="eq-bar h-full w-[3px] bg-signal [animation-delay:-0.6s]" />
                    <span className="eq-bar h-2/3 w-[3px] bg-signal [animation-delay:-0.3s]" />
                  </span>
                ) : (
                  <span className="w-5 shrink-0 text-center text-xs tabular-nums text-text-tertiary">
                    {i + 1}
                  </span>
                )}
                <span className="min-w-0 flex-1">
                  <p
                    className={`m-0 truncate text-sm font-semibold ${
                      i === chapterIndex ? "text-signal" : "text-text-primary"
                    }`}
                  >
                    {c.title}
                  </p>
                  <p className="m-0 mt-0.5 text-[11px] text-text-tertiary">{c.sourceName}</p>
                </span>
                <span className="shrink-0 text-xs text-text-tertiary">
                  {formatSecondsAsClock(c.durationSec)}
                </span>
              </button>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
