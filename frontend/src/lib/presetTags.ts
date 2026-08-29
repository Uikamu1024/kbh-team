import tagsConfig from "@/config/tags.json";

// プリセットタグ一覧は src/config/tags.json から読み込む。
// バックエンドの config/tags.json（LLMがタグ判定する際の許可集合）と
// 内容を1文字違わず一致させること。ずれると、そのタグを選んだユーザーに
// 記事が一切マッチしなくなる（あるいはLLMが付けたタグをどのユーザーも
// 選べなくなる）。
export const PRESET_TAGS = tagsConfig.tags;

// オンボーディング／プロフィールのタグ編集で共通の選択上限
// （frontend/docs/features/onboarding.md「選択できるテーマは2〜3個まで」に準拠）。
export const MIN_TAGS = 2;
export const MAX_TAGS = 3;
