import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { putUserTags } from "@/lib/api";
import { useUserId } from "@/lib/useUserId";
import { setDisplayName } from "@/lib/user";
import { useGenerateProgram } from "@/lib/useGenerateProgram";
import { GENERATING_STATUS_MESSAGES, GENERATION_STAGE_ORDER } from "@/lib/generationStage";
import { MIN_TAGS } from "@/lib/presetTags";
import { TagPicker } from "@/components/TagPicker";

type Step = "name" | "tags" | "generating";

export default function Onboarding() {
  const navigate = useNavigate();
  const { userId, loading: userLoading, error: userError } = useUserId();
  const [step, setStep] = useState<Step>("name");
  const [name, setName] = useState("");
  const [selectedTags, setSelectedTags] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { error: generateError, stage, generate } = useGenerateProgram();
  const statusIndex = stage ? GENERATION_STAGE_ORDER.indexOf(stage) : 0;

  const canSubmitName = name.trim().length > 0;
  const canSubmitTags = !!userId && selectedTags.length >= MIN_TAGS && !submitting;

  async function handleTagsSubmit() {
    if (!userId || !canSubmitTags) return;
    setSubmitting(true);
    setError(null);
    try {
      await putUserTags(userId, selectedTags);
      setDisplayName(name.trim());
      setStep("generating");
    } catch {
      setError("テーマの保存に失敗しました。もう一度お試しください。");
      setSubmitting(false);
    }
  }

  // 生成中画面: 見た目上の進捗ステータス（statusIndex）は、裏で実行する本物の
  // 生成（regenerateLatestProgram。通常はバックグラウンドで進む非同期API、
  // DEMO_MODE時のみ同期的に即完了する）をuseGenerateProgramがポーリングして
  // 取得した実際の進行段階と連動する。完了/失敗どちらでも一旦ホームへ進める
  // （失敗時はホーム側の「今すぐ生成する」ボタンで再試行できる）。
  useEffect(() => {
    if (step !== "generating") return;
    // その日の最初の1本は「作り直し」ではないため、1日3回までのリセット上限に
    // カウントしない（useGenerateProgram参照）。
    generate(() => navigate("/home", { replace: true }), { bypassResetLimit: true });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step]);

  useEffect(() => {
    if (step === "generating" && generateError) {
      navigate("/home", { replace: true });
    }
  }, [step, generateError, navigate]);

  if (userLoading) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <p className="text-sm text-text-tertiary">起動しています…</p>
      </div>
    );
  }

  if (userError) {
    return (
      <div className="flex flex-1 items-center justify-center px-8 text-center">
        <p className="text-sm text-text-secondary">
          バックエンドに接続できませんでした。backendが起動しているか確認してください。
        </p>
      </div>
    );
  }

  if (step === "name") {
    return (
      <div className="flex flex-1 flex-col px-6 pb-8 pt-6">
        <p className="mb-5 text-xs font-extrabold tracking-wider text-signal">
          はじめての設定 01 / 02
        </p>
        <h1 className="text-[27px] font-bold leading-snug tracking-tight">
          まずは、あなたの
          <br />
          名前を教えてください。
        </h1>
        <p className="mt-3.5 text-sm leading-relaxed text-text-secondary">
          朝の番組で呼びかける名前として使います。
        </p>

        <form
          className="mt-10 flex flex-col"
          onSubmit={(e) => {
            e.preventDefault();
            if (canSubmitName) setStep("tags");
          }}
        >
          <label className="mb-2 text-sm font-bold" htmlFor="userName">
            表示名
          </label>
          <input
            id="userName"
            type="text"
            maxLength={20}
            autoComplete="name"
            autoFocus
            placeholder="例：たろう"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className="min-h-[52px] rounded-xl border border-bg-elevated-3 bg-bg-elevated px-3.5 py-3 text-base text-text-primary outline-none focus:border-signal focus:ring-2 focus:ring-signal-soft"
          />
          <button
            type="submit"
            disabled={!canSubmitName}
            className="mt-7 min-h-[50px] rounded-xl bg-signal text-[15px] font-extrabold text-white transition-opacity disabled:cursor-not-allowed disabled:bg-bg-elevated-3 disabled:text-text-tertiary"
          >
            次へ
          </button>
        </form>
      </div>
    );
  }

  if (step === "tags") {
    return (
      <div className="flex flex-1 flex-col px-6 pb-8 pt-6">
        <p className="mb-5 text-xs font-extrabold tracking-wider text-signal">
          はじめての設定 02 / 02
        </p>
        <h1 className="text-[27px] font-bold leading-snug tracking-tight">
          気になるテーマを
          <br />
          2〜3個選んでください。
        </h1>
        <p className="mt-3.5 text-sm leading-relaxed text-text-secondary">
          選んだテーマをもとに、毎朝の番組をつくります。
        </p>

        <div className="mt-7.5">
          <TagPicker selected={selectedTags} onChange={setSelectedTags} />
        </div>
        <p className="mt-3.5 text-xs text-text-secondary">
          {selectedTags.length} / 3 選択中
        </p>

        {error && <p className="mt-4 text-sm text-danger">{error}</p>}

        <button
          type="button"
          disabled={!canSubmitTags}
          onClick={handleTagsSubmit}
          className="mt-auto min-h-[50px] rounded-xl bg-signal text-[15px] font-extrabold text-white transition-opacity disabled:cursor-not-allowed disabled:bg-bg-elevated-3 disabled:text-text-tertiary"
        >
          {submitting ? "保存しています…" : "番組をはじめる"}
        </button>
      </div>
    );
  }

  // step === "generating"
  return (
    <div className="flex flex-1 flex-col justify-center px-6 pb-8 pt-6">
      <div
        className="generating-loader mb-8 flex items-center gap-1.5"
        aria-hidden="true"
      >
        <span className="h-2.5 w-2.5 rounded-full bg-signal" />
        <span className="h-2.5 w-2.5 rounded-full bg-signal" />
        <span className="h-2.5 w-2.5 rounded-full bg-signal" />
      </div>
      <p className="text-xs font-extrabold tracking-wider text-signal">
        今日の番組を準備中
      </p>
      <h1 className="mt-2.5 text-[27px] font-bold leading-snug tracking-tight">
        あなた向けのニュースを
        <br />
        集めています。
      </h1>
      <p className="mt-3.5 text-sm leading-relaxed text-text-secondary">
        {selectedTags.join("・")}から、今朝の話題をまとめています。
      </p>
      <p className="mt-2 text-xs text-text-tertiary">
        通常1〜6分ほどかかります。この画面を閉じたり更新したりしても生成は続くので、
        後でまた開いて確認してください
      </p>

      <div className="mt-8.5 flex flex-col gap-3 border-t border-bg-elevated-3 pt-4.5" aria-live="polite">
        {GENERATION_STAGE_ORDER.map((stageKey, index) => (
          <p
            key={stageKey}
            className={`relative m-0 pl-5.5 text-xs leading-relaxed ${
              index <= statusIndex ? "text-text-primary" : "text-text-tertiary"
            }`}
          >
            <span
              className={`absolute left-0.5 top-[5px] h-2.5 w-2.5 rounded-full border ${
                index <= statusIndex
                  ? "border-signal bg-signal"
                  : "border-text-tertiary bg-transparent"
              }`}
            />
            {GENERATING_STATUS_MESSAGES[stageKey]}
          </p>
        ))}
      </div>
    </div>
  );
}
