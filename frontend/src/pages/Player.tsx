import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { getAudioUrl, getLatestProgram, getProgram } from "@/lib/api";
import { useUserId } from "@/lib/useUserId";
import { formatSecondsAsClock } from "@/lib/format";
import { concatenateWavBuffers } from "@/lib/wav";
import type { Program } from "@/lib/types";

// バックエンドはチャプターごとに別々の音声ファイルを配信するが（audioUrl参照）、
// <audio>のsrcをチャプターごとに切り替えて連続再生しようとすると、切り替えの
// たびにplay()を呼び直す必要がありブラウザの自動再生制限で止まってしまう。
// そのため全チャプターの音声を一度取得し、1本のWAVに結合して単一の<audio>で
// 再生する（最初の1回のplay()だけで最後まで通しで再生できる）。
type LoadState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "ready"; program: Program; audioUrl: string; chapterDurations: number[] };

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
  const [searchParams] = useSearchParams();
  const programIdParam = searchParams.get("programId");
  const autoplay = searchParams.get("autoplay") === "1";

  const { userId } = useUserId();
  const [result, setResult] = useState<{ key: string; state: LoadState } | null>(null);
  const [isPlaying, setIsPlaying] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [attempt, setAttempt] = useState(0);
  const audioRef = useRef<HTMLAudioElement>(null);
  const objectUrlRef = useRef<string | null>(null);
  const requestKey = `${programIdParam}:${userId}:${attempt}`;

  useEffect(() => {
    let cancelled = false;
    const key = `${programIdParam}:${userId}:${attempt}`;

    const load = programIdParam
      ? getProgram(programIdParam)
      : userId
        ? getLatestProgram(userId)
        : null;

    if (!load) return;

    load
      .then(async (program) => {
        if (program.chapters.length === 0) throw new Error("no chapters");
        const buffers = await Promise.all(
          program.chapters.map((c) =>
            fetch(getAudioUrl(program.id, c.id)).then((res) => {
              if (!res.ok) throw new Error(`audio fetch failed: ${res.status}`);
              return res.arrayBuffer();
            }),
          ),
        );
        if (cancelled) return;
        const { blob, chapterDurations } = concatenateWavBuffers(buffers);

        if (objectUrlRef.current) URL.revokeObjectURL(objectUrlRef.current);
        const audioUrl = URL.createObjectURL(blob);
        objectUrlRef.current = audioUrl;

        setResult({ key, state: { status: "ready", program, audioUrl, chapterDurations } });
        setCurrentTime(0);
      })
      .catch(() => {
        if (!cancelled) setResult({ key, state: { status: "error" } });
      });

    return () => {
      cancelled = true;
    };
  }, [programIdParam, userId, attempt]);

  useEffect(() => {
    return () => {
      if (objectUrlRef.current) URL.revokeObjectURL(objectUrlRef.current);
    };
  }, []);

  const state: LoadState = result?.key === requestKey ? result.state : { status: "loading" };

  const program = state.status === "ready" ? state.program : null;
  const chapterDurations = state.status === "ready" ? state.chapterDurations : [];
  // 各チャプターの結合トラック内での開始時刻（秒）。boundaries[i] <= currentTime
  // となる最後のiが現在のチャプター。チャプター数は数件程度なのでメモ化はせず
  // 毎レンダー計算する。
  const chapterBoundaries: number[] = [];
  {
    let acc = 0;
    for (const duration of chapterDurations) {
      chapterBoundaries.push(acc);
      acc += duration;
    }
  }

  let chapterIndex = 0;
  for (let i = 0; i < chapterBoundaries.length; i++) {
    if (currentTime >= chapterBoundaries[i]) chapterIndex = i;
  }

  const chapter = program?.chapters[chapterIndex] ?? null;
  const chapterStart = chapterBoundaries[chapterIndex] ?? 0;
  const chapterElapsed = Math.max(0, currentTime - chapterStart);
  const lines = useMemo(
    () => (chapter ? chapter.script.split("\n").filter((line) => line.trim() !== "") : []),
    [chapter],
  );
  const waveform = useMemo(
    () => (chapter ? buildWaveformHeights(chapter.id) : []),
    [chapter],
  );

  // 結合済みの音声が新しく読み込まれた直後、autoplay指定があれば1回だけ再生開始する。
  // 以降チャプターが進んでも同じ<audio>が鳴り続けるだけなのでplay()の再呼び出しは不要
  // （チャプターごとにsrcを切り替えていた旧実装では、この再呼び出しが自動再生制限に
  // 引っかかって連続再生できなかった）。
  useEffect(() => {
    if (state.status !== "ready" || !audioRef.current) return;
    if (autoplay) {
      audioRef.current.play().catch(() => setIsPlaying(false));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.status === "ready" ? state.audioUrl : null]);

  function togglePlay() {
    const audio = audioRef.current;
    if (!audio) return;
    if (audio.paused) {
      audio.play().catch(() => {});
    } else {
      audio.pause();
    }
  }

  function goToChapter(index: number) {
    const audio = audioRef.current;
    if (!audio || chapterBoundaries.length === 0) return;
    const total = chapterBoundaries.length;
    const target = ((index % total) + total) % total;
    audio.currentTime = chapterBoundaries[target];
    setCurrentTime(chapterBoundaries[target]);
  }

  function handleEnded() {
    setIsPlaying(false);
  }

  function handleSeek(ratio: number) {
    const audio = audioRef.current;
    const duration = chapterDurations[chapterIndex];
    if (!audio || !chapter || duration === undefined) return;
    audio.currentTime = chapterStart + ratio * duration;
  }

  if (state.status === "loading") {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-text-tertiary">読み込んでいます…</p>
      </div>
    );
  }

  if (state.status === "error" || !program || !chapter) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 text-center">
        <p className="text-sm text-text-secondary">番組の取得に失敗しました。</p>
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

  const chapterDuration = chapterDurations[chapterIndex] ?? chapter.durationSec;
  const progressRatio = chapterDuration > 0 ? chapterElapsed / chapterDuration : 0;
  const activeLineIndex = Math.min(
    lines.length - 1,
    Math.floor(progressRatio * lines.length),
  );
  const playedBars = Math.round(waveform.length * progressRatio);

  return (
    <div className="pt-2">
      <audio
        ref={audioRef}
        src={state.status === "ready" ? state.audioUrl : undefined}
        onTimeUpdate={(e) => setCurrentTime(e.currentTarget.currentTime)}
        onPlay={() => setIsPlaying(true)}
        onPause={() => setIsPlaying(false)}
        onEnded={handleEnded}
      />

      <div
        className="relative flex overflow-hidden rounded-[22px] p-6"
        style={{
          aspectRatio: "1 / 0.82",
          background: "linear-gradient(160deg, #1c4d33, #0e2a1c 55%, #0a1a12)",
        }}
      >
        <div
          className="pointer-events-none absolute -inset-[40%] blur-[10px]"
          style={{
            background:
              "radial-gradient(circle at 70% 20%, rgba(34, 197, 94, 0.35), transparent 60%)",
          }}
        />
        <div className="relative z-[1] flex flex-col justify-end gap-2.5">
          {lines.map((line, i) => (
            <p
              key={i}
              className={`m-0 font-semibold leading-relaxed transition-all duration-300 ${
                i === activeLineIndex
                  ? "translate-x-0.5 text-[17px] text-white"
                  : "text-[15px] text-white/35"
              }`}
            >
              {line}
            </p>
          ))}
        </div>
      </div>

      <div className="mt-5">
        <a
          href={chapter.sourceUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="text-xs lowercase text-text-tertiary hover:text-text-secondary"
        >
          {chapter.sourceName}
        </a>
        <h2 className="m-0 mt-1 text-[19px] font-bold leading-snug">{chapter.title}</h2>
        <p className="m-0 mt-1.5 text-xs text-text-secondary">
          {chapterIndex + 1} / {program.chapters.length}
        </p>
      </div>

      <div className="mt-5">
        <div
          className="flex h-10 cursor-pointer items-center gap-[2.5px] py-1"
          onClick={(e) => {
            const rect = e.currentTarget.getBoundingClientRect();
            handleSeek(Math.min(1, Math.max(0, (e.clientX - rect.left) / rect.width)));
          }}
        >
          {waveform.map((h, i) => (
            <div
              key={i}
              className={`min-w-[2px] flex-1 rounded-sm transition-colors ${
                i < playedBars ? "bg-accent" : "bg-bg-elevated-3"
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

      <div className="my-1.5 flex items-center justify-center gap-7">
        <button
          type="button"
          aria-label="前の記事"
          onClick={() => goToChapter(chapterIndex - 1)}
          className="flex items-center justify-center text-text-primary transition-transform hover:opacity-80 active:scale-90"
        >
          <svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor">
            <path d="M6 6h2v12H6zm3.5 6l8.5 6V6z" />
          </svg>
        </button>
        <button
          type="button"
          aria-label={isPlaying ? "一時停止" : "再生"}
          onClick={togglePlay}
          className="flex h-[58px] w-[58px] items-center justify-center rounded-full bg-white text-[#0d0d0f] transition-transform hover:scale-[1.04] active:scale-95"
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
          onClick={() => goToChapter(chapterIndex + 1)}
          className="flex items-center justify-center text-text-primary transition-transform hover:opacity-80 active:scale-90"
        >
          <svg viewBox="0 0 24 24" width="22" height="22" fill="currentColor">
            <path d="M16 6h2v12h-2zM6 6l8.5 6L6 18z" />
          </svg>
        </button>
      </div>

      <section>
        <h2 className="mb-3 mt-5 text-sm font-bold">この番組のチャプター</h2>
        <ul className="flex flex-col gap-1">
          {program.chapters.map((c, i) => (
            <li key={c.id}>
              <button
                type="button"
                onClick={() => goToChapter(i)}
                className={`flex w-full items-center gap-3 rounded-lg px-2.5 py-2.5 text-left transition-colors hover:bg-bg-elevated ${
                  i === chapterIndex ? "bg-bg-elevated" : ""
                }`}
              >
                {i === chapterIndex ? (
                  <span className="flex h-3.5 w-5 shrink-0 items-end justify-center gap-0.5">
                    <span className="eq-bar h-2/5 w-[3px] bg-accent" />
                    <span className="eq-bar h-full w-[3px] bg-accent [animation-delay:-0.6s]" />
                    <span className="eq-bar h-2/3 w-[3px] bg-accent [animation-delay:-0.3s]" />
                  </span>
                ) : (
                  <span className="w-5 shrink-0 text-center text-xs tabular-nums text-text-tertiary">
                    {i + 1}
                  </span>
                )}
                <span className="min-w-0 flex-1">
                  <p
                    className={`m-0 truncate text-sm font-semibold ${
                      i === chapterIndex ? "text-accent" : "text-text-primary"
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
