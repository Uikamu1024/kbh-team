import { createUser } from "./api";

// userId はバックエンドが発行したUUIDをlocalStorageに保存して使い回す
// （docs/api-contract.yaml の「userIdの扱い」参照。ログイン機能はスコープ外）。
const USER_ID_KEY = "tsugaku-radio:userId";

// 表示名はログイン機能がスコープ外のためAPIの対象外。クライアントローカルにのみ保持する
// （docs/api-contract.yaml の「表示名（ニックネーム）の扱い」参照）。
const DISPLAY_NAME_KEY = "tsugaku-radio:displayName";
const DEFAULT_DISPLAY_NAME = "ゲスト";

export function getStoredUserId(): string | null {
  return window.localStorage.getItem(USER_ID_KEY);
}

function setStoredUserId(userId: string): void {
  window.localStorage.setItem(USER_ID_KEY, userId);
}

// 初回起動時はユーザーが未作成のため、必ずサーバーへ発行を依頼してから保存する
// （クライアントが自前でUUIDを生成しない理由は api-contract.yaml 参照）。
export async function ensureUserId(): Promise<string> {
  const existing = getStoredUserId();
  if (existing) return existing;

  const { userId } = await createUser();
  setStoredUserId(userId);
  return userId;
}

export function getDisplayName(): string {
  return window.localStorage.getItem(DISPLAY_NAME_KEY) ?? DEFAULT_DISPLAY_NAME;
}

export function setDisplayName(name: string): void {
  window.localStorage.setItem(DISPLAY_NAME_KEY, name);
}
