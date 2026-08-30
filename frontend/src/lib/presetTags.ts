import tags from "../../../config/tags.json";

// プリセットタグ一覧はリポジトリ直下の config/tags.json から直接読み込む
// （フロントエンド側に複製を持たない。ビルド時にバンドルされる）。
// バックエンドの cmd/ingest もこの同じファイルを読み、LLMがタグ判定する際の
// 許可集合として使う。単一の情報源なので、フロントとバックエンドでズレる
// ことはない。
export const PRESET_TAGS: string[] = tags;

// オンボーディング／プロフィールのタグ編集で共通の選択上限
// （frontend/docs/features/onboarding.md「選択できるテーマは2〜3個まで」に準拠）。
export const MIN_TAGS = 2;
export const MAX_TAGS = 3;
