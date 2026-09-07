import type {
  ApiErrorBody,
  ApiErrorCode,
  CreateUserResponse,
  GenerationStatusResponse,
  HealthResponse,
  Program,
  ProgramHistoryResponse,
  RegenerateAcceptedResponse,
  SettingsRequest,
  UserProfile,
} from "./types";

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

const DEFAULT_TIMEOUT_MS = 10_000;

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

// 202 Acceptedを即座に返す非同期API。実際の生成はバックグラウンドで進み、
// 進行状況・完了・失敗はgetGenerationStatusをポーリングして確認する
// （useGenerateProgram参照）。
export function regenerateLatestProgram(userId: string): Promise<RegenerateAcceptedResponse> {
  return apiFetch<RegenerateAcceptedResponse>(`/api/users/${userId}/programs/latest/regenerate`, {
    method: "POST",
  });
}

export function getGenerationStatus(userId: string): Promise<GenerationStatusResponse> {
  return apiFetch<GenerationStatusResponse>(`/api/users/${userId}/generation-status`);
}

export function getProgram(programId: string): Promise<Program> {
  return apiFetch<Program>(`/api/programs/${programId}`);
}

export function getAudioUrl(programId: string, chapterId: string): string {
  return `${API_BASE_URL}/api/audio/${programId}/${chapterId}`;
}

export function getHealth(): Promise<HealthResponse> {
  return apiFetch<HealthResponse>("/api/health");
}
