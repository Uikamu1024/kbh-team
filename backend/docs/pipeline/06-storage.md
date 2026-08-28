# ⑦ 保存
**対応ディレクトリ**: `/backend/internal/db`（PostgreSQL）、`/backend/internal/storage`（音声ファイル）

## 概要
生成した番組（台本＋音声）をPostgreSQLとローカルファイルシステムに保存する。DB・ファイルへアクセスするのはバックエンド（Go）のみで、フロントエンドは直接アクセスせずバックエンドAPI経由で取得する。

PostgreSQLはDockerでローカル起動する（[docker-compose.yml](../../../Directory%20structure.md)参照）。

## PostgreSQLスキーマ（案）
```sql
CREATE TABLE users (
  id UUID PRIMARY KEY,
  tags TEXT[] NOT NULL DEFAULT '{}'   -- 選択したテーマタグ
);

CREATE TABLE programs (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE chapters (
  id UUID PRIMARY KEY,
  program_id UUID NOT NULL REFERENCES programs(id),
  position INT NOT NULL,              -- 番組内の並び順
  title TEXT NOT NULL,
  source_url TEXT NOT NULL,
  source_name TEXT NOT NULL,
  script TEXT NOT NULL,
  audio_path TEXT NOT NULL,           -- ローカルファイルシステム上のパス
  importance_score INT NOT NULL
);
```

## 音声ファイルの保存先
- `/backend/data/audio/{programId}/{chapterId}.mp3`（チャプターごと）のようにローカルファイルシステムへ保存
- `chapters.audio_path`にファイルパスを保存し、バックエンドAPIが配信する（例：`GET /api/audio/{programId}/{chapterId}`）

## 関連
- 前のステップ：[⑥音声化](./05-tts.md)
- `chapters.source_url` / `chapters.audio_path`は[プレイヤー](../../../frontend/docs/features/player.md)がバックエンドAPI経由で取得して使用
- `users.tags`は[オンボーディング](../../../frontend/docs/features/onboarding.md)がバックエンドAPI経由で書き込む
- APIの形状は[docs/api-contract.md](../../../docs/api-contract.md)を参照
