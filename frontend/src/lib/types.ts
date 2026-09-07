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
  | "ARTICLE_CACHE_EMPTY"
  | "NO_UNSEEN_ARTICLES"
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
  // scriptを改行分割した各行（空行除去前のインデックス）が実際に発話開始する、
  // チャプター先頭からの経過秒数。本フィールド追加前に生成された番組では空配列
  // になるため、フロント側は文字数比按分へフォールバックする（Player.tsx参照）。
  lineStartOffsetsSec: number[];
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

export interface RegenerateAcceptedResponse {
  acceptedAt: string;
}

export interface HealthResponse {
  status: "ok" | "error";
  postgres: "ok" | "error";
  voicevox: "ok" | "error";
}

export type GenerationStage = "collecting" | "scripting" | "finishing";

export interface GenerationLastError {
  code: ApiErrorCode;
  message: string;
}

export interface GenerationStatusResponse {
  generating: boolean;
  stage?: GenerationStage;
  lastError?: GenerationLastError;
}
