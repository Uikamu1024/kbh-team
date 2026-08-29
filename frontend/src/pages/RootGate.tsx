import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ApiError, getUser } from "@/lib/api";
import { useUserId } from "@/lib/useUserId";
import { clearStoredUserId, ensureUserId } from "@/lib/user";

// アプリの入口。userIdの発行確認と、オンボーディング済みかどうかの判定だけを行い、
// 適切な画面へリダイレクトする（このルート自体はUIを持たない）。
export default function RootGate() {
  const navigate = useNavigate();
  const { userId, loading: userLoading, error: userError } = useUserId();
  const [error, setError] = useState<Error | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!userId) return;
    let cancelled = false;

    getUser(userId)
      .then((profile) => {
        if (cancelled) return;
        navigate(profile.tags.length > 0 ? "/home" : "/onboarding", { replace: true });
      })
      .catch(async (err) => {
        if (cancelled) return;

        // DBリセット等でlocalStorageのuserIdがサーバー側に存在しなくなっているケース。
        // 古いIDを捨てて新規発行し直せば、通常のオンボーディングとして復旧できる。
        if (err instanceof ApiError && err.code === "USER_NOT_FOUND") {
          clearStoredUserId();
          try {
            await ensureUserId();
            if (!cancelled) navigate("/onboarding", { replace: true });
          } catch (retryErr) {
            if (!cancelled) {
              setError(retryErr instanceof Error ? retryErr : new Error(String(retryErr)));
            }
          }
          return;
        }

        setError(err instanceof Error ? err : new Error(String(err)));
      });

    return () => {
      cancelled = true;
    };
  }, [userId, navigate, attempt]);

  const combinedError = userError ?? error;

  if (combinedError) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-4 px-8 text-center">
        <p className="text-sm text-text-secondary">
          バックエンドに接続できませんでした。
          <br />
          backendが起動しているか確認してください。
        </p>
        <button
          type="button"
          onClick={() => {
            setError(null);
            setAttempt((n) => n + 1);
          }}
          className="rounded-full bg-accent px-5 py-2 text-sm font-semibold text-[#06120a]"
        >
          再試行
        </button>
      </div>
    );
  }

  return (
    <div className="flex flex-1 items-center justify-center">
      <p className="text-sm text-text-tertiary">
        {userLoading ? "起動しています…" : "読み込んでいます…"}
      </p>
    </div>
  );
}
