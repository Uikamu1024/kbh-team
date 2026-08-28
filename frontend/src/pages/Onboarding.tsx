import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { putUserTags } from "@/lib/api";
import { useUserId } from "@/lib/useUserId";
import { MIN_TAGS } from "@/lib/presetTags";
import { TagPicker } from "@/components/TagPicker";

export default function Onboarding() {
  const navigate = useNavigate();
  const { userId, loading: userLoading, error: userError } = useUserId();
  const [selected, setSelected] = useState<string[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canSubmit = !!userId && selected.length >= MIN_TAGS && !submitting;

  async function handleSubmit() {
    if (!userId || !canSubmit) return;
    setSubmitting(true);
    setError(null);
    try {
      await putUserTags(userId, selected);
      navigate("/home", { replace: true });
    } catch {
      setError("テーマの保存に失敗しました。もう一度お試しください。");
    } finally {
      setSubmitting(false);
    }
  }

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

  return (
    <div className="flex flex-1 flex-col px-6 pb-8 pt-14">
      <h1 className="text-2xl font-bold leading-snug">
        気になるテーマを
        <br />
        選んでください
      </h1>
      <p className="mt-3 text-sm text-text-secondary">
        選んだテーマをもとに、毎朝ラジオを作ります（2〜3個まで）
      </p>

      <div className="mt-8">
        <TagPicker selected={selected} onChange={setSelected} />
      </div>

      {error && <p className="mt-6 text-sm text-danger">{error}</p>}

      <button
        type="button"
        disabled={!canSubmit}
        onClick={handleSubmit}
        className="mt-auto w-full rounded-full bg-accent py-4 text-[15px] font-bold text-[#06120a] transition-opacity disabled:cursor-not-allowed disabled:opacity-35"
      >
        {submitting ? "保存しています…" : "はじめる"}
      </button>
    </div>
  );
}
