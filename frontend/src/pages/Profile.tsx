import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ApiError, getUser, putUserSettings, putUserTags } from "@/lib/api";
import { useUserId } from "@/lib/useUserId";
import { useGenerateProgram } from "@/lib/useGenerateProgram";
import { GENERATING_STATUS_MESSAGES } from "@/lib/generationStage";
import { getDisplayName, setDisplayName as saveDisplayName } from "@/lib/user";
import { applyTheme, getStoredTheme, type Theme } from "@/lib/theme";
import { MIN_TAGS } from "@/lib/presetTags";
import { TagPicker } from "@/components/TagPicker";
import type { UserProfile } from "@/lib/types";

// コンテスト提出用に一時的に非表示にしている（バックエンド・API・DBは未変更、
// trueに戻すだけで復活する）。配信時刻：cronによる毎朝の自動生成は実際には
// 稼働させていない（ローカル完結の設計方針上、常時稼働サーバーが前提になる
// ため）。提出書類側で「本番運用時はサーバーを常時稼働させるかクラウド配置が
// 必要」と説明する方針とし、UIには出さない。
const SHOW_DELIVERY_TIME_SETTING = false;
// 番組の長さは表示する。ただし外部APIの利用上限（提出時は無料枠キーを使う
// ため）により、選んだ分数通りの長さにならないことがあるため、注意書きを
// 添えている（下記LENGTH_SETTING_NOTE）。上限の高いAPIキーに切り替えれば
// 解消する（提出書類に記載。キー自体は提出物に含めない）。
const SHOW_LENGTH_SETTING = true;
const LENGTH_SETTING_NOTE =
  "※ 外部APIの利用上限により、選んだ長さより短くなる場合があります";

const LENGTH_OPTIONS = [5, 10, 15] as const;
const THEME_OPTIONS: { value: Theme; label: string }[] = [
  { value: "light", label: "ライト" },
  { value: "dark", label: "ダーク" },
];

type LoadState = { status: "loading" } | { status: "error" } | { status: "ready" };

export default function Profile() {
  const navigate = useNavigate();
  const { userId } = useUserId();
  const [result, setResult] = useState<{ key: string; status: "ready" | "error" } | null>(null);
  const [profile, setProfile] = useState<UserProfile | null>(null);
  const [tagEditorOpen, setTagEditorOpen] = useState(false);
  const [tags, setTags] = useState<string[]>([]);
  const [deliveryTime, setDeliveryTime] = useState("06:00");
  const [lengthMinutes, setLengthMinutes] = useState<5 | 10 | 15>(10);
  const [toast, setToast] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [displayName, setDisplayNameState] = useState(getDisplayName());
  const [nameEditorOpen, setNameEditorOpen] = useState(false);
  const [nameDraft, setNameDraft] = useState(displayName);
  const [theme, setTheme] = useState<Theme>(getStoredTheme());
  const isFirstTagsRender = useRef(true);
  const requestKey = `${userId}:${attempt}`;
  const { generating, error: generateError, stage, generate } = useGenerateProgram();
  const previousGeneratingRef = useRef(generating);
  const previousGenerateErrorRef = useRef<string | null>(null);

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
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.code === "USER_NOT_FOUND") {
          // DBリセット等でuserIdが存在しなくなっている。入口（RootGate）の
          // 自動復旧に任せる（古いIDを破棄して新規発行→オンボーディングへ）。
          navigate("/", { replace: true });
          return;
        }
        setResult({ key, status: "error" });
      });

    return () => {
      cancelled = true;
    };
  }, [userId, attempt, navigate]);

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

  // 生成中フラグ（generating）はuseGenerateProgram側でGET .../generation-status
  // を見て管理しているため、この画面を離れて戻ってきても（＝この画面のローカル
  // stateが一度失われても）サーバー側が実際に生成中かどうかを引き継いで正しく
  // 表示できる。完了判定もそちらに任せ、ここではgeneratingがtrue→falseに
  // 変わったタイミングでプロフィール（リセット回数・統計）を再取得するだけにする
  // （このボタン以外の場所・画面から開始された生成が裏で終わった場合も拾える）。
  useEffect(() => {
    if (previousGeneratingRef.current && !generating) {
      setAttempt((n) => n + 1);
    }
    previousGeneratingRef.current = generating;
  }, [generating]);

  useEffect(() => {
    if (generateError && generateError !== previousGenerateErrorRef.current) {
      showToast(generateError);
    }
    previousGenerateErrorRef.current = generateError;
  }, [generateError]);

  function handleReset() {
    if (!userId || !profile) return;
    generate(() => showToast("今日の番組を作り直しました"));
  }

  function handleThemeChange(next: Theme) {
    applyTheme(next);
    setTheme(next);
  }

  function openNameEditor() {
    setNameDraft(displayName);
    setNameEditorOpen(true);
  }

  function handleSaveName() {
    const trimmed = nameDraft.trim();
    const next = trimmed === "" ? displayName : trimmed;
    saveDisplayName(next);
    setDisplayNameState(next);
    setNameEditorOpen(false);
    showToast("表示名を変更しました");
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
          className="rounded-full bg-accent px-5 py-2 text-sm font-semibold text-white"
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
        <div className="flex h-14 w-14 shrink-0 items-center justify-center rounded-full bg-accent text-xl font-bold text-white">
          {displayName.slice(0, 1)}
        </div>
        <div className="min-w-0 flex-1">
          {nameEditorOpen ? (
            <div className="mb-1.5 flex items-center gap-2">
              <input
                type="text"
                value={nameDraft}
                onChange={(e) => setNameDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleSaveName();
                  if (e.key === "Escape") setNameEditorOpen(false);
                }}
                maxLength={20}
                autoFocus
                className="min-w-0 flex-1 rounded-lg border border-bg-elevated-3 bg-bg-elevated-2 px-2.5 py-1.5 text-sm font-semibold text-text-primary"
              />
              <button
                type="button"
                onClick={handleSaveName}
                className="shrink-0 rounded-full bg-signal px-3 py-1.5 text-xs font-semibold text-white"
              >
                保存
              </button>
            </div>
          ) : (
            <button
              type="button"
              onClick={openNameEditor}
              className="mb-1.5 flex items-center gap-1.5 border-0 bg-transparent p-0 text-left text-base font-bold text-text-primary hover:text-accent"
            >
              {displayName}
              <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 20h9" />
                <path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z" />
              </svg>
            </button>
          )}
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

        {SHOW_DELIVERY_TIME_SETTING && (
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
            />
          </div>
        )}

        {SHOW_LENGTH_SETTING && (
          <div className="border-t border-bg-elevated-3 py-4">
            <div className="flex items-center justify-between gap-4">
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
                        ? "bg-signal text-white"
                        : "text-text-secondary hover:text-text-primary"
                    }`}
                  >
                    {minutes}分
                  </button>
                ))}
              </div>
            </div>
            <p className="m-0 mt-2.5 text-xs text-text-tertiary">{LENGTH_SETTING_NOTE}</p>
          </div>
        )}

        <div
          className={`flex items-center justify-between gap-4 py-4 ${
            SHOW_DELIVERY_TIME_SETTING || SHOW_LENGTH_SETTING ? "border-t border-bg-elevated-3" : ""
          }`}
        >
          <div className="min-w-0">
            <p className="m-0 mb-0.5 text-sm font-semibold">外観</p>
            <p className="m-0 text-xs text-text-tertiary">画面の配色を切り替えます</p>
          </div>
          <div className="flex shrink-0 gap-0.5 rounded-full bg-bg-elevated-2 p-[3px]">
            {THEME_OPTIONS.map((option) => (
              <button
                key={option.value}
                type="button"
                onClick={() => handleThemeChange(option.value)}
                className={`rounded-full px-3 py-1.5 text-xs font-semibold transition-colors ${
                  theme === option.value
                    ? "bg-signal text-white"
                    : "text-text-secondary hover:text-text-primary"
                }`}
              >
                {option.label}
              </button>
            ))}
          </div>
        </div>

        <div className="border-t border-bg-elevated-3 py-4">
          <div className="flex items-center justify-between gap-4">
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
              disabled={resetsRemaining <= 0 || generating}
              onClick={handleReset}
              className="shrink-0 rounded-full border border-danger px-3.5 py-2 text-[13px] font-semibold text-danger transition-colors hover:bg-danger hover:text-white disabled:cursor-not-allowed disabled:border-bg-elevated-3 disabled:text-text-tertiary disabled:hover:bg-transparent"
            >
              {generating ? GENERATING_STATUS_MESSAGES[stage ?? "collecting"] : "作り直す"}
            </button>
          </div>
          {generating && (
            <p className="m-0 mt-2.5 text-xs text-text-tertiary">
              通常1〜6分ほどかかります。画面を閉じたり更新したりしても生成は続くので、
              後でまた開いて確認してください
            </p>
          )}
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
