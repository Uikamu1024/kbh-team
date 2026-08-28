# APIコントラクト（フロントエンド⇔バックエンド）

フロントエンドとバックエンドを並行開発するための取り決め。**実装より先にこのファイルを更新して合意すること**。変更したら両チームに共有する。

- ベースURL：`NEXT_PUBLIC_API_BASE_URL`（[frontend/CLAUDE.md](../frontend/CLAUDE.md#環境変数)参照、ローカルでは`http://localhost:8080`想定）
- すべてのリクエスト／レスポンスボディは`Content-Type: application/json; charset=utf-8`（音声配信エンドポイントを除く）
- 認証は行わない（ログイン機能はスコープ外。[userIdの扱い](#useridの扱い)を参照）。ローカル完結・オフライン前提の構成であり、本番公開は想定していないための割り切り
- データモデルは[06-storage.md](../backend/docs/pipeline/06-storage.md)のPostgreSQLスキーマに対応する

## CORS
フロントエンド（`http://localhost:3000`想定）とバックエンド（`http://localhost:8080`想定）はオリジンが異なるため、バックエンドは以下のCORSレスポンスヘッダを返すこと。実装時に抜けやすく、抜けると疎通確認の最初の一歩で詰まるので明記しておく。

```
Access-Control-Allow-Origin: http://localhost:3000
Access-Control-Allow-Methods: GET, POST, PUT, OPTIONS
Access-Control-Allow-Headers: Content-Type
```

## エラーレスポンス形式
2xx以外はすべて以下の形式で返す。

```json
{
  "error": {
    "code": "PROGRAM_NOT_FOUND",
    "message": "指定されたユーザーの番組がまだ生成されていません"
  }
}
```

| HTTPステータス | 用途 |
| --- | --- |
| `400 Bad Request` | リクエストボディ・パラメータの形式が不正（例：`tags`が空配列、UUID形式が不正） |
| `404 Not Found` | 指定した`userId`/`programId`/`chapterId`が存在しない、または番組が未生成 |
| `502 Bad Gateway` | 外部プロバイダ（jina.ai／firecrawl／Gemini／VOICEVOX）の呼び出しに失敗した |
| `500 Internal Server Error` | 上記以外のサーバー内部エラー |

`code`の値は`SCREAMING_SNAKE_CASE`の固定文字列とし、エンドポイントごとの一覧は各セクションに記載する。

## userIdの扱い
ログイン機能はスコープ外のため、次の方式で暫定的にユーザーを識別する。

1. フロントエンドは初回起動時に[`POST /api/users`](#post-apiusers)を呼び、サーバーが発行した`userId`（UUID）を受け取る
2. `userId`をブラウザの`localStorage`に保存し、以降の全リクエストで使い回す
3. サーバーはリクエストされた`userId`が`users`テーブルに存在するかを検証し、存在しなければ`404`（`USER_NOT_FOUND`）を返す

クライアントが自前でUUIDを生成しない理由：`programs.user_id`が外部キー制約付きのため、存在しないIDでの書き込みを防ぐにはサーバー発行が確実。

---

## POST /api/users
匿名ユーザーを1件作成し、`userId`を発行する（アプリ初回起動時に呼ぶ）。

リクエストボディ：なし

レスポンス：`201 Created`
```json
{ "userId": "user-uuid" }
```

エラー：なし（サーバー内部エラー以外は発生しない想定）

---

## PUT /api/users/{userId}/tags
テーマタグを保存する（[オンボーディング](../frontend/docs/features/onboarding.md)）。全件を置き換える冪等な操作のため`PUT`とする（追加ではなく上書き）。

リクエスト:
```json
{ "tags": ["AI", "京都"] }
```
- `tags`：1〜3件の文字列配列。プリセットタグ一覧との照合は現状バックエンド側では行わない（未決定事項参照）

レスポンス: `204 No Content`

エラー：
| `code` | 状況 |
| --- | --- |
| `USER_NOT_FOUND` | `userId`が存在しない（`404`） |
| `INVALID_TAGS` | `tags`が空、4件以上、または要素が空文字列（`400`） |

curl例：
```bash
curl -X PUT http://localhost:8080/api/users/$USER_ID/tags \
  -H "Content-Type: application/json" \
  -d '{"tags": ["AI", "京都"]}'
```

---

## GET /api/users/{userId}/programs/latest
その日（最新）の番組を取得する（[ホーム](../frontend/docs/features/home.md)）。バッチ処理（[POST /api/batch/run](#post-apibatchrun)）が生成した、その`userId`にとって最も新しい番組を返す。

レスポンス：`200 OK`
```json
{
  "id": "program-uuid",
  "createdAt": "2026-08-29T06:00:00+09:00",
  "totalDurationSec": 420,
  "chapters": [
    {
      "id": "chapter-uuid",
      "position": 0,
      "title": "〇〇について",
      "sourceUrl": "https://example.com/article",
      "sourceName": "Example News",
      "audioUrl": "/api/audio/program-uuid/chapter-uuid",
      "durationSec": 95,
      "importanceScore": 5
    }
  ]
}
```
- `durationSec` / `totalDurationSec`：チャプター単体／番組合計の再生時間（秒）。プレイヤーのシークバー初期表示に使う（`<audio>`のメタデータ読み込み完了を待たずに表示できるようにするため）

エラー：
| `code` | 状況 |
| --- | --- |
| `USER_NOT_FOUND` | `userId`が存在しない（`404`） |
| `PROGRAM_NOT_FOUND` | 番組がまだ1件も生成されていない（`404`）。ホーム画面はこれを「準備中」表示の分岐に使う |

---

## GET /api/programs/{programId}
指定した番組の詳細を取得する（[プレイヤー](../frontend/docs/features/player.md)）。レスポンス形式は[`GET /api/users/{userId}/programs/latest`](#get-apiusersuseridprogramslatest)と同じ（`userId`に紐付かない直接アクセス用。ホームからプレイヤーへ遷移する際に番組IDで参照する）。

エラー：`PROGRAM_NOT_FOUND`（`404`）

---

## GET /api/audio/{programId}/{chapterId}
音声ファイルを配信する。実体は[⑦保存](../backend/docs/pipeline/06-storage.md)のローカルファイルシステムから読む。

- レスポンスヘッダ：`Content-Type: audio/wav`
  - VOICEVOXの出力がネイティブでWAVのため、フォーマット変換（MP3エンコード等）はMVPでは行わずWAVのまま配信する。ファイル容量は大きくなるが、追加の依存（ffmpeg等）を増やさずに済むトレードオフを取る
- `Range`リクエストヘッダに対応する（`206 Partial Content`）。HTML5 `<audio>`のシーク操作が`Range`リクエストを送るため、これがないとシークバー操作で毎回頭出しになる

エラー：`AUDIO_NOT_FOUND`（`404`。`chapters`テーブルに該当行はあるがファイルが物理的に存在しない場合を含む）

---

## POST /api/demo/generate
デモ用の軽量パイプラインをその場で実行する（記事数を絞った縮小版。[backend/CLAUDE.md](../backend/CLAUDE.md#デモ用の軽量パイプライン)参照）。

**この呼び出しはDBに保存しない。** `programs`/`chapters`テーブルへの書き込みは行わず、生成した結果をレスポンスとしてその場で返すだけの一発実行。理由：`programs.user_id`は必須の外部キーだが、このエンドポイントは`userId`を受け取らない想定のライブデモ用途のため、永続化の対象外として設計をシンプルに保つ。

リクエスト:
```json
{ "tags": ["AI"] }
```
- `tags`：1件以上

レスポンス: `200 OK`。`GET /api/programs/{programId}`と同じ形式だが、`id`はレスポンスを跨いで再利用できない一時的な値（`demo-`プレフィックス付きなど）を返す。音声ファイル自体は`AUDIO_STORAGE_PATH`配下に一時保存し、`audioUrl`経由で配信する（保存期間・掃除タイミングは未決定事項）

**同期実行・低いタイムアウト前提の注意**：記事取得→LLM→TTSを直列に呼ぶため、レスポンスまで数十秒〜1分程度かかる。フロントエンドはこの間ローディング状態を表示し、`fetch`のタイムアウトは余裕を持って60秒以上に設定すること

エラー：
| `code` | 状況 |
| --- | --- |
| `INVALID_TAGS` | `tags`が空（`400`） |
| `UPSTREAM_FETCH_FAILED` | 記事取得（jina.ai／firecrawl）が両方とも失敗（`502`） |
| `UPSTREAM_LLM_FAILED` | Gemini API呼び出し失敗・レート制限（`502`） |
| `UPSTREAM_TTS_FAILED` | VOICEVOX ENGINEへの接続失敗（`502`） |

---

## POST /api/batch/run
毎朝6:00相当のバッチ処理をローカルのcron/手動実行から叩くためのエンドポイント（[backend/CLAUDE.md](../backend/CLAUDE.md#バッチ実行)参照）。全ユーザー分の番組を生成し、`programs`/`chapters`に保存する（[POST /api/demo/generate](#post-apidemogenerate)とは異なり、こちらは永続化する）。

リクエストボディ：なし

レスポンス: `202 Accepted`（非同期実行のため。処理はバックグラウンドで継続し、このレスポンスは受理のみを示す）
```json
{ "acceptedAt": "2026-08-29T06:00:00+09:00" }
```

**進捗確認手段はMVPでは提供しない**：`202`を返した後、各ユーザーの生成完了は個別に[`GET /api/users/{userId}/programs/latest`](#get-apiusersuseridprogramslatest)をポーリングして`createdAt`の更新を見るしかない。バッチ全体の完了通知や失敗ユーザーの一覧化は行わない（ハッカソン規模ではオーバースペックと判断）

**想定される呼び出し元はローカルのcron／開発者本人のみ**：認証を行わないため、外部に公開しない前提（[CORS](#cors)の許可オリジンにも含めない）

---

## GET /api/health
`docker compose up`後、Postgres・VOICEVOX ENGINEへの接続確認も含めたヘルスチェック。フロントエンド起動前の疎通確認や、開発時の動作確認に使う。

レスポンス: `200 OK`
```json
{ "status": "ok", "postgres": "ok", "voicevox": "ok" }
```
いずれかに接続できない場合は`503 Service Unavailable`で該当箇所を`"error"`にして返す。

---

## 未決定・要相談
- プリセットタグ一覧の管理場所（フロントエンドのハードコード／バックエンドでマスタ管理）と、`PUT /api/users/{userId}/tags`でのバリデーション有無
- `POST /api/demo/generate`が一時保存する音声ファイルの掃除タイミング（TTL／起動時クリーンアップ等）
- `POST /api/batch/run`の同時実行防止（多重起動をどう防ぐか。ロックファイル／DBフラグ等）
