import { useEffect, useState } from "react";
import { ensureUserId } from "./user";

interface UseUserIdResult {
  userId: string | null;
  loading: boolean;
  error: Error | null;
}

// 画面ごとにuserIdの発行待ちを個別に扱わなくて済むよう共通化したフック。
// 2回目以降はlocalStorageにキャッシュされたuserIdを同期的に返すため、
// POST /api/usersが呼ばれるのは初回起動時の1回だけになる。
export function useUserId(): UseUserIdResult {
  const [userId, setUserId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    let cancelled = false;
    ensureUserId()
      .then((id) => {
        if (!cancelled) setUserId(id);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err : new Error(String(err)));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return { userId, loading, error };
}
