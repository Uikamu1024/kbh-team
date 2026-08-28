# APIコントラクト（フロントエンド⇔バックエンド）

フロントエンドとバックエンドを並行開発するための取り決め。**実装より先にこのファイルを更新して合意すること**。変更したら両チームに共有する。

ベースURL：`NEXT_PUBLIC_API_BASE_URL`（[frontend/.env.example](../frontend/.env.example)、ローカルでは`http://localhost:8080`想定）

データモデルは[06-storage.md](../backend/docs/pipeline/06-storage.md)のPostgreSQLスキーマに対応する。

## POST /api/users/{userId}/tags
テーマタグを保存する（[オンボーディング](../frontend/docs/features/onboarding.md)）。

リクエスト:
```json
{ "tags": ["AI", "京都"] }
```
レスポンス: `204 No Content`

## GET /api/programs/latest?userId={userId}
その日（最新）の番組を取得する（[ホーム](../frontend/docs/features/home.md)）。

レスポンス:
```json
{
  "id": "program-uuid",
  "createdAt": "2026-08-29T06:00:00+09:00",
  "chapters": [
    {
      "id": "chapter-uuid",
      "position": 0,
      "title": "〇〇について",
      "sourceUrl": "https://example.com/article",
      "sourceName": "Example News",
      "audioUrl": "/api/audio/program-uuid/chapter-uuid",
      "importanceScore": 5
    }
  ]
}
```
番組が未生成の場合は`404 Not Found`。

## GET /api/programs/{programId}
指定した番組の詳細を取得する（[プレイヤー](../frontend/docs/features/player.md)）。レスポンス形式は`GET /api/programs/latest`と同じ。

## GET /api/audio/{programId}/{chapterId}
音声ファイルを配信する（`Content-Type: audio/mpeg`等）。実体は[⑦保存](../backend/docs/pipeline/06-storage.md)のローカルファイルシステムから読む。

## POST /api/demo/generate
デモ用の軽量パイプラインをその場で実行する（記事数を絞った縮小版。[backend/CLAUDE.md](../backend/CLAUDE.md)の「デモ用の軽量パイプライン」参照）。

リクエスト:
```json
{ "tags": ["AI"] }
```
レスポンス: `GET /api/programs/latest`と同じ形式（生成した番組をそのまま返す）。

## POST /api/batch/run
毎朝6:00相当のバッチ処理をローカルのcron/手動実行から叩くためのエンドポイント（[backend/CLAUDE.md](../backend/CLAUDE.md)の未決定事項「バッチ実行の自動化方法」参照）。全ユーザー分の番組を生成する。

レスポンス: `202 Accepted`（非同期実行のため）

## 未決定・要相談
- エラーレスポンスの共通形式（`{ "error": "..." }`等）を決める
- `userId`の発行方法（ログイン機能はMVPスコープ外のため、暫定的にどう識別するか）
