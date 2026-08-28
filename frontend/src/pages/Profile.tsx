import { useEffect, useRef, useState } from "react";
import { ApiError, getUser, putUserSettings, putUserTags, regenerateLatestProgram } from "@/lib/api";
import { useUserId } from "@/lib/useUserId";
import { getDisplayName } from "@/lib/user";
import { MIN_TAGS } from "@/lib/presetTags";
import { TagPicker } from "@/components/TagPicker";
import type { UserProfile } from "@/lib/types";

const LENGTH_OPTIONS = [5, 10, 15] as const;

type LoadState = { status: "loading" } | { status: "error" } | { status: "ready" };

export default function Profile() {
  const { userId } = useUserId();
  const [result, setResult] = useState<{ key: string; status: "ready" | "error" } | null>(null);
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [tagEditorOpen, setTagEditorOpen] = useState(false);
  const [tags, setTags] = useState<string[]>([]);
  const [deliveryTime, setDeliveryTime] = useState("06:00");
  const [lengthMinutes, setLengthMinutes] = useState<5 | 10 | 15>(10);
  const [toast, setToast] = useState<string | null>(null);
  const [resetting, setResetting] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const isFirstTagsRender = useRef(true);
  const requestKey = `${userId}:${attempt}`;

  useEffect(() => {
    if (!userId) return;
    let cancelled = false;
    const key = `${userId}:${attempt}`;

    getUser(userId)
      .then((p) => {
        if (cancelled) return;
        setProfile(p);
        setTags(p.tags);
        setDeliveryTime(p.deliveryTime);
        setLengthMinutes(p.lengthMinutes);
        isFirstTagsRender.current = true;
        setResult({ key, status: "ready" });
      })
      .catch(() => {
        if (!cancelled) setResult({ key, status: "error" });
      });

    return () => {
      cancelled = true;
    };
  }, [userId, attempt]);

  const state: LoadState =
    result?.key === requestKey ? { status: result.status } : { status: "loading" };

  function showToast(message: string) {
    setToast(message);
    window.setTimeout(() => setToast((current) => (current === message ? null : current)), 2000);
  }

  // タグは2〜3個の範囲を満たすたびに自動保存する
  useEffect(() => {
    if (isFirstTagsRender.current) {
      isFirstTagsRender.current = false;
      return;
    }
    if (!userId || tags.length < MIN_TAGS) return;
    putUserTags(userId, tags)
      .then(() => showToast(`テーマを更新しました（${tags.join("、")}）`))
      .catch(() => showToast("テーマの保存に失敗しました"));
  }, [tags, userId]);

  async function saveSettings(next: { deliveryTime: string; lengthMinutes: 5 | 10 | 15 }) {
    if (!userId) return;
    try {
      await putUserSettings(userId, next);
      showToast("設定を保存しました");
    } catch {
      showToast("設定の保存に失敗しました");
    }
  }

  async function handleReset() {
    if (!userId || !profile || resetting) return;
    setResetting(true);
    try {
      await regenerateLatestProgram(userId);
      const refreshed = await getUser(userId);
      setProfile(refreshed);
      showToast("今日の番組を作り直しました");
    } catch (err) {
      if (err instanceof ApiError && err.code === "RESET_LIMIT_EXCEEDED") {
        showToast("本日の上限に達しています");
      } else {
        showToast("作り直しに失敗しました");
      }
    } finally {
      setResetting(false);
    }
  }

  if (state.status === "loading") {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-text-tertiary">読み込んでいます…</p>
      </div>
    );
  }

  if (state.status === "error" || !profile) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 text-center">
        <p className="text-sm text-text-secondary">プロフィールの取得に失敗しました。</p>
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

  const resetsRemaining = profile.resetsLimitPerDay - profile.resetsUsedToday;

  return (
    <div className="pt-4">
      <section className="flex items-center gap-3.5">
        <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-full bg-gradient-to-br from-accent to-[#0f3d24] text-xl font-bold text-[#06120a]">
          {getDisplayName().slice(0, 1)}
        </div>
        <div>
          <p className="m-0 mb-1.5 text-base font-bold">{getDisplayName()}</p>
          <button
            type="button"
            onClick={() => setTagEditorOpen((v) => !v)}
            className="inline-flex items-center gap-1 border-0 bg-transparent p-0 text-[13px] text-text-secondary hover:text-text-primary"
          >
            興味・関心を設定
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M9 18l6-6-6-6" />
            </svg>
          </button>
        </div>
      </section>

      {tagEditorOpen && (
        <section className="mt-5 rounded-2xl bg-bg-elevated p-4">
          <p className="mb-3 text-xs text-text-tertiary">2〜3個まで選択できます</p>
          <TagPicker selected={tags} onChange={setTags} />
        </section>
      )}

      <section className="mt-7 rounded-2xl bg-bg-elevated px-4">
        <h2 className="mb-3 mt-5 text-sm font-bold">設定</h2>

        <div className="flex items-center justify-between gap-4 py-4">
          <div className="min-w-0">
            <p className="m-0 mb-0.5 text-sm font-semibold">配信時刻</p>
            <p className="m-0 text-xs text-text-tertiary">毎朝この時刻に番組を用意します</p>
          </div>
          <input
            type="time"
            value={deliveryTime}
            onChange={(e) => {
              setDeliveryTime(e.target.value);
              saveSettings({ deliveryTime: e.target.value, lengthMinutes });
            }}
            className="shrink-0 rounded-lg border border-bg-elevated-3 bg-bg-elevated-2 px-2.5 py-1.5 text-sm font-semibold text-text-primary"
            style={{ colorScheme: "dark" }}
          />
        </div>

        <div className="flex items-center justify-between gap-4 border-t border-bg-elevated-3 py-4">
          <div className="min-w-0">
            <p className="m-0 mb-0.5 text-sm font-semibold">ポッドキャストの長さ</p>
            <p className="m-0 text-xs text-text-tertiary">番組全体の目安時間</p>
          </div>
          <div className="flex shrink-0 gap-0.5 rounded-full bg-bg-elevated-2 p-[3px]">
            {LENGTH_OPTIONS.map((minutes) => (
              <button
                key={minutes}
                type="button"
                onClick={() => {
                  setLengthMinutes(minutes);
                  saveSettings({ deliveryTime, lengthMinutes: minutes });
                }}
                className={`rounded-full px-3 py-1.5 text-xs font-semibold transition-colors ${
                  lengthMinutes === minutes
                    ? "bg-accent text-[#06120a]"
                    : "text-text-secondary hover:text-text-primary"
                }`}
              >
                {minutes}分
              </button>
            ))}
          </div>
        </div>

        <div className="flex items-center justify-between gap-4 border-t border-bg-elevated-3 py-4">
          <div className="min-w-0">
            <p className="m-0 mb-0.5 text-sm font-semibold">今日の番組をリセット</p>
            <p className="m-0 text-xs text-text-tertiary">
              {resetsRemaining > 0
                ? `今日の番組を作り直します（残り${resetsRemaining}回）`
                : "本日の上限に達しました（明日リセットされます）"}
            </p>
          </div>
          <button
            type="button"
            disabled={resetsRemaining <= 0 || resetting}
            onClick={handleReset}
            className="shrink-0 rounded-full border border-danger px-3.5 py-2 text-[13px] font-semibold text-danger transition-colors hover:bg-danger hover:text-white disabled:cursor-not-allowed disabled:border-bg-elevated-3 disabled:text-text-tertiary disabled:hover:bg-transparent"
          >
            {resetting ? "作り直しています…" : "作り直す"}
          </button>
        </div>
      </section>

      <section className="mt-7 flex gap-3">
        <div className="flex-1 rounded-2xl bg-bg-elevated p-4 text-center">
          <p className="m-0 mb-1 text-2xl font-bold text-accent">{profile.stats.totalPrograms}</p>
          <p className="m-0 text-[11px] text-text-tertiary">配信された番組</p>
        </div>
        <div className="flex-1 rounded-2xl bg-bg-elevated p-4 text-center">
          <p className="m-0 mb-1 text-2xl font-bold text-accent">
            {Math.round(profile.stats.totalDurationSec / 60)}
          </p>
          <p className="m-0 text-[11px] text-text-tertiary">合計収録時間（分）</p>
        </div>
      </section>

      {toast && (
        <div className="fixed bottom-24 left-1/2 -translate-x-1/2 rounded-full bg-bg-elevated-3 px-4.5 py-2.5 text-[13px] shadow-[0_8px_24px_rgba(0,0,0,0.4)]">
          {toast}
        </div>
      )}
    </div>
  );
}
