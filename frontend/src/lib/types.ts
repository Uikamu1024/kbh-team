// バックエンドAPIのレスポンス/リクエスト型。
// docs/api-contract.yaml のスキーマと1対1で対応させる。型を変更する場合は
// 先に api-contract.yaml を更新してから、この型を追従させること。

export type ApiErrorCode =
  | "USER_NOT_FOUND"
  | "INVALID_TAGS"
  | "INVALID_SETTINGS"
  | "PROGRAM_NOT_FOUND"
  | "AUDIO_NOT_FOUND"
  | "ALREADY_GENERATING"
  | "RESET_LIMIT_EXCEEDED"
  | "UPSTREAM_FETCH_FAILED"
  | "UPSTREAM_LLM_FAILED"
  | "UPSTREAM_TTS_FAILED"
  | "DEMO_MODE_DISABLED"
  | "INTERNAL_ERROR";

export interface ApiErrorBody {
  error: {
    code: ApiErrorCode;
    message: string;
  };
}

export interface CreateUserResponse {
  userId: string;
}

export interface TagsRequest {
  tags: string[];
}

export interface SettingsRequest {
  deliveryTime: string; // "HH:MM"
  lengthMinutes: 5 | 10 | 15;
}

export interface UserProfile {
  userId: string;
  tags: string[];
  deliveryTime: string; // "HH:MM"
  lengthMinutes: 5 | 10 | 15;
  resetsUsedToday: number;
  resetsLimitPerDay: number;
  stats: {
    totalPrograms: number;
    totalDurationSec: number;
  };
}

export interface Chapter {
  id: string;
  position: number;
  title: string;
  sourceUrl: string;
  sourceName: string;
  script: string;
  audioUrl: string;
  durationSec: number;
  importanceScore: number;
}

export interface Program {
  id: string;
  title: string;
  createdAt: string;
  greetingText: string;
  changeCount: number;
  totalDurationSec: number;
  chapters: Chapter[];
}

export interface ProgramSummary {
  id: string;
  title: string;
  createdAt: string;
  totalDurationSec: number;
}

export interface ProgramHistoryResponse {
  items: ProgramSummary[];
  total: number;
}

export interface BatchRunResponse {
  acceptedAt: string;
}

export interface HealthResponse {
  status: "ok" | "error";
  postgres: "ok" | "error";
  voicevox: "ok" | "error";
}
