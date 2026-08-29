import type {
  ApiErrorBody,
  ApiErrorCode,
  BatchRunResponse,
  CreateUserResponse,
  HealthResponse,
  Program,
  ProgramHistoryResponse,
  SettingsRequest,
  UserProfile,
} from "./types";

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

// 記事取得→LLM→TTSを直列実行するエンドポイント（作り直し）は
// docs/api-contract.yaml 上、数十秒〜1分程度かかる想定のため長めに取る。
const DEFAULT_TIMEOUT_MS = 10_000;
const LONG_RUNNING_TIMEOUT_MS = 90_000;

export class ApiError extends Error {
  code: ApiErrorCode;
  status: number;

  constructor(status: number, code: ApiErrorCode, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

async function apiFetch<T>(
  path: string,
  init: RequestInit = {},
  timeoutMs: number = DEFAULT_TIMEOUT_MS,
): Promise<T> {
  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), timeoutMs);

  let res: Response;
  try {
    res = await fetch(`${API_BASE_URL}${path}`, {
      ...init,
      signal: controller.signal,
      headers: {
        ...(init.body ? { "Content-Type": "application/json" } : {}),
        ...init.headers,
      },
    });
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") {
      throw new ApiError(0, "INTERNAL_ERROR", "サーバーへの接続がタイムアウトしました");
    }
    throw new ApiError(0, "INTERNAL_ERROR", "サーバーに接続できませんでした");
  } finally {
    clearTimeout(timeoutId);
  }

  if (res.status === 204) {
    return undefined as T;
  }

  if (!res.ok) {
    let body: ApiErrorBody | null = null;
    try {
      body = (await res.json()) as ApiErrorBody;
    } catch {
      // レスポンスボディがJSONでない場合はそのままフォールバックへ
    }
    throw new ApiError(
      res.status,
      body?.error.code ?? "INTERNAL_ERROR",
      body?.error.message ?? "サーバーでエラーが発生しました",
    );
  }

  return (await res.json()) as T;
}

export function createUser(): Promise<CreateUserResponse> {
  return apiFetch<CreateUserResponse>("/api/users", { method: "POST" });
}

export function getUser(userId: string): Promise<UserProfile> {
  return apiFetch<UserProfile>(`/api/users/${userId}`);
}

export function putUserTags(userId: string, tags: string[]): Promise<void> {
  return apiFetch<void>(`/api/users/${userId}/tags`, {
    method: "PUT",
    body: JSON.stringify({ tags }),
  });
}

export function putUserSettings(
  userId: string,
  settings: SettingsRequest,
): Promise<void> {
  return apiFetch<void>(`/api/users/${userId}/settings`, {
    method: "PUT",
    body: JSON.stringify(settings),
  });
}

export function getLatestProgram(userId: string): Promise<Program> {
  return apiFetch<Program>(`/api/users/${userId}/programs/latest`);
}

export function listPrograms(
  userId: string,
  limit?: number,
): Promise<ProgramHistoryResponse> {
  const query = limit ? `?limit=${limit}` : "";
  return apiFetch<ProgramHistoryResponse>(
    `/api/users/${userId}/programs${query}`,
  );
}

export function regenerateLatestProgram(userId: string): Promise<Program> {
  return apiFetch<Program>(
    `/api/users/${userId}/programs/latest/regenerate`,
    { method: "POST" },
    LONG_RUNNING_TIMEOUT_MS,
  );
}

export function getProgram(programId: string): Promise<Program> {
  return apiFetch<Program>(`/api/programs/${programId}`);
}

// デモ用：1日1回の制限を回避して番組をもう1本追加生成する
// （既存の最新番組は置き換えず、履歴に追加される）。
export function createAdditionalProgram(userId: string): Promise<Program> {
  return apiFetch<Program>(
    `/api/users/${userId}/programs`,
    { method: "POST" },
    LONG_RUNNING_TIMEOUT_MS,
  );
}

export function getAudioUrl(programId: string, chapterId: string): string {
  return `${API_BASE_URL}/api/audio/${programId}/${chapterId}`;
}

export function getHealth(): Promise<HealthResponse> {
  return apiFetch<HealthResponse>("/api/health");
}

// 開発・デモ用：全ユーザー分の番組をまとめて生成するバッチを起動する
// （本来は毎朝6:00相当のcron想定のエンドポイント。「自分の分だけ」生成する
// 専用APIはまだ無いため、ホーム画面の手動生成ボタンから暫定的に使う）。
export function runBatch(): Promise<BatchRunResponse> {
  return apiFetch<BatchRunResponse>("/api/batch/run", { method: "POST" });
}
