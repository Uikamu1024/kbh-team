import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useNavigate } from "react-router-dom";
import { ApiError, getAudioUrl, getLatestProgram, getProgram } from "./api";
import { useUserId } from "./useUserId";
import { concatenateWavBuffers } from "./wav";
import type { Program } from "./types";

// 再生状態をルート（Home/Player/Profile）をまたいで共有するためのContext。
// AppShell配下にProviderを置き、<audio>要素もここで1つだけ保持することで、
// タブを行き来しても再生が途切れず、ホーム/プロフィールのミニプレイヤーにも
// 同じ状態を表示できる。

type Source = { type: "latest" } | { type: "program"; programId: string };

type LoadState =
  | { status: "loading" }
  | { status: "error" }
  | { status: "not-ready" }
  | { status: "ready"; program: Program; audioUrl: string; chapterDurations: number[] };

// 未読み込み時のchapterDurationsとして使う、常に同一参照の空配列
// （毎レンダー新しい[]を返すとuseMemo/useCallbackの依存が不安定になるため）
const EMPTY_DURATIONS: number[] = [];

interface PlaybackContextValue {
  source: Source | null;
  status: LoadState["status"];
  program: Program | null;
  chapterIndex: number;
  chapterDurations: number[];
  currentTime: number;
  chapterElapsed: number;
  isPlaying: boolean;
  loadLatest: (opts?: { autoplay?: boolean }) => void;
  loadProgram: (programId: string, opts?: { autoplay?: boolean }) => void;
  retry: () => void;
  play: () => void;
  pause: () => void;
  toggle: () => void;
  goToChapter: (index: number) => void;
  seek: (ratio: number) => void;
}

const PlaybackContext = createContext<PlaybackContextValue | null>(null);

export function usePlayback(): PlaybackContextValue {
  const ctx = useContext(PlaybackContext);
  if (!ctx) throw new Error("usePlayback must be used within PlaybackProvider");
  return ctx;
}

export function PlaybackProvider({ children }: { children: ReactNode }) {
  const navigate = useNavigate();
  const { userId } = useUserId();
  const [source, setSource] = useState<Source | null>(null);
  const [result, setResult] = useState<{ key: string; state: LoadState } | null>(null);
  const [isPlaying, setIsPlaying] = useState(false);
  const [currentTime, setCurrentTime] = useState(0);
  const [attempt, setAttempt] = useState(0);
  const audioRef = useRef<HTMLAudioElement>(null);
  const objectUrlRef = useRef<string | null>(null);
  const autoplayRef = useRef(false);

  const requestKey = source
    ? `${source.type}:${source.type === "program" ? source.programId : userId}:${attempt}`
    : null;

  useEffect(() => {
    if (!source) return;
    let cancelled = false;
    const key = `${source.type}:${source.type === "program" ? source.programId : userId}:${attempt}`;

    const load =
      source.type === "program"
        ? getProgram(source.programId)
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
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.code === "PROGRAM_NOT_FOUND") {
          setResult({ key, state: { status: "not-ready" } });
        } else if (err instanceof ApiError && err.code === "USER_NOT_FOUND") {
          navigate("/", { replace: true });
        } else {
          setResult({ key, state: { status: "error" } });
        }
      });

    return () => {
      cancelled = true;
    };
  }, [source, userId, attempt, navigate]);

  useEffect(() => {
    return () => {
      if (objectUrlRef.current) URL.revokeObjectURL(objectUrlRef.current);
    };
  }, []);

  const state: LoadState =
    result?.key === requestKey && requestKey ? result.state : { status: source ? "loading" : "loading" };

  // 音声が新しく読み込み完了したタイミングで、autoplay指定があれば1回だけ再生する
  useEffect(() => {
    if (state.status === "ready" && autoplayRef.current) {
      autoplayRef.current = false;
      audioRef.current?.play().catch(() => setIsPlaying(false));
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [state.status === "ready" ? state.audioUrl : null]);

  const loadLatest = useCallback((opts?: { autoplay?: boolean }) => {
    if (opts?.autoplay) autoplayRef.current = true;
    setSource({ type: "latest" });
    setAttempt((n) => n + 1);
  }, []);

  const loadProgram = useCallback((programId: string, opts?: { autoplay?: boolean }) => {
    if (opts?.autoplay) autoplayRef.current = true;
    setSource({ type: "program", programId });
    setAttempt((n) => n + 1);
  }, []);

  const retry = useCallback(() => setAttempt((n) => n + 1), []);

  const play = useCallback(() => {
    audioRef.current?.play().catch(() => {});
  }, []);
  const pause = useCallback(() => {
    audioRef.current?.pause();
  }, []);
  const toggle = useCallback(() => {
    const audio = audioRef.current;
    if (!audio) return;
    if (audio.paused) play();
    else pause();
  }, [play, pause]);

  const program = state.status === "ready" ? state.program : null;
  const chapterDurations = state.status === "ready" ? state.chapterDurations : EMPTY_DURATIONS;
  // chapterDurationsの参照が変わったときだけ再計算する（毎レンダー新しい配列に
  // ならないよう、この先のuseCallback依存を安定させるため。「準備中」の場合は
  // 常にEMPTY_DURATIONSという同一参照を返すことで安定させている）
  const chapterBoundaries = useMemo(() => {
    const boundaries: number[] = [];
    let acc = 0;
    for (const duration of chapterDurations) {
      boundaries.push(acc);
      acc += duration;
    }
    return boundaries;
  }, [chapterDurations]);
  let chapterIndex = 0;
  for (let i = 0; i < chapterBoundaries.length; i++) {
    if (currentTime >= chapterBoundaries[i]) chapterIndex = i;
  }
  const chapterStart = chapterBoundaries[chapterIndex] ?? 0;
  const chapterElapsed = Math.max(0, currentTime - chapterStart);

  const goToChapter = useCallback(
    (index: number) => {
      const audio = audioRef.current;
      if (!audio || chapterBoundaries.length === 0) return;
      const total = chapterBoundaries.length;
      const target = ((index % total) + total) % total;
      audio.currentTime = chapterBoundaries[target];
      setCurrentTime(chapterBoundaries[target]);
    },
    [chapterBoundaries],
  );

  const seek = useCallback(
    (ratio: number) => {
      const audio = audioRef.current;
      const duration = chapterDurations[chapterIndex];
      if (!audio || duration === undefined) return;
      audio.currentTime = chapterStart + ratio * duration;
    },
    [chapterDurations, chapterIndex, chapterStart],
  );

  function handleEnded() {
    setIsPlaying(false);
  }

  const value = useMemo<PlaybackContextValue>(
    () => ({
      source,
      status: state.status,
      program,
      chapterIndex,
      chapterDurations,
      currentTime,
      chapterElapsed,
      isPlaying,
      loadLatest,
      loadProgram,
      retry,
      play,
      pause,
      toggle,
      goToChapter,
      seek,
    }),
    [
      source,
      state.status,
      program,
      chapterIndex,
      chapterDurations,
      currentTime,
      chapterElapsed,
      isPlaying,
      loadLatest,
      loadProgram,
      retry,
      play,
      pause,
      toggle,
      goToChapter,
      seek,
    ],
  );

  return (
    <PlaybackContext.Provider value={value}>
      <audio
        ref={audioRef}
        src={state.status === "ready" ? state.audioUrl : undefined}
        onTimeUpdate={(e) => setCurrentTime(e.currentTarget.currentTime)}
        onPlay={() => setIsPlaying(true)}
        onPause={() => setIsPlaying(false)}
        onEnded={handleEnded}
      />
      {children}
    </PlaybackContext.Provider>
  );
}
