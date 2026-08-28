// プリセットタグ一覧はフロントエンドでハードコードして管理する
// （docs/api-contract.yaml の x-open-questions:
//  「プリセットタグ一覧の管理場所」は未決定だが、mock/app.js と同じ方針でひとまず決め打ちする）。
export const PRESET_TAGS = [
  "AI",
  "京都",
  "ゲーム",
  "音楽",
  "スポーツ",
  "ビジネス",
  "映画",
  "旅行",
] as const;

// オンボーディング／プロフィールのタグ編集で共通の選択上限
// （frontend/docs/features/onboarding.md「選択できるテーマは2〜3個まで」に準拠）。
export const MIN_TAGS = 2;
export const MAX_TAGS = 3;
