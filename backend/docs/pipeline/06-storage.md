# ⑦ 保存
**対応ディレクトリ**: `/backend/internal/db`（PostgreSQL）、`/backend/internal/storage`（音声ファイル）

## 概要
生成した番組（台本＋音声）をPostgreSQLとローカルファイルシステムに保存する。DB・ファイルへアクセスするのはバックエンド（Go）のみで、フロントエンドは直接アクセスせずバックエンドAPI経由で取得する。

PostgreSQLはDockerでローカル起動する（[docker-compose.yml](../../../Directory%20structure.md)参照）。

## 入力
- `userId: uuid`
- `greetingText: string` / `changeCount: int`（[④重要度判定](./03-score.md)・[⑤要約・台本化](./04-script.md)の出力）
- `chapters: []ChapterAudio`（[⑥音声化](./05-tts.md)の出力）

`POST /api/demo/generate`から呼ばれた場合はこのステップ自体をスキップする（DBに書き込まない。[backend/docs/api-handlers.md](../api-handlers.md)参照）。

## PostgreSQLスキーマ（案）
```sql
CREATE TABLE users (
  id UUID PRIMARY KEY,
  tags TEXT[] NOT NULL DEFAULT '{}',           -- 選択したテーマタグ
  delivery_time TIME NOT NULL DEFAULT '06:00', -- 配信時刻（プロフィール画面の設定）
  length_minutes INT NOT NULL DEFAULT 10,      -- ポッドキャストの長さ（5・10・15のいずれか）
  reset_count INT NOT NULL DEFAULT 0,          -- 本日の「今日の番組をリセット」実行回数（上限3）
  reset_date DATE                              -- reset_countが対象としている日付。配信時刻を跨いだら0にリセット
);

CREATE TABLE programs (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  greeting_text TEXT NOT NULL,   -- 冒頭挨拶文。API応答のgreetingTextに対応（[04-script.md](./04-script.md)参照）
  change_count INT NOT NULL      -- 前日からの差分件数。API応答のchangeCountに対応（[03-score.md](./03-score.md)参照）
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
  duration_sec INT NOT NULL,          -- 音声の長さ（秒）。API応答のdurationSecに対応
  importance_score INT NOT NULL
);
```

## 音声ファイルの保存先
- `/backend/data/audio/{programId}/{chapterId}.wav`（チャプターごと）のようにローカルファイルシステムへ保存。VOICEVOXの出力がネイティブでWAVのため、MVPではMP3変換を行わずWAVのまま保存・配信する（フォーマット変換を挟まない分、追加の依存ライブラリが増えない）
- `chapters.audio_path`にファイルパスを保存し、バックエンドAPIが配信する（例：`GET /api/audio/{programId}/{chapterId}`、`Content-Type: audio/wav`）
- `duration_sec`はTTS生成時（[⑥音声化](./05-tts.md)）に音声の長さを計測して書き込む

## 関連
- 前のステップ：[⑥音声化](./05-tts.md)
- `chapters.source_url` / `chapters.audio_path` / `chapters.script`は[プレイヤー](../../../frontend/docs/features/player.md)がバックエンドAPI経由で取得して使用
- `users.tags`は[オンボーディング](../../../frontend/docs/features/onboarding.md)がバックエンドAPI経由で書き込む
- `users.delivery_time` / `users.length_minutes` / `users.reset_count` / `users.reset_date`はプロフィール画面の設定・「今日の番組をリセット」機能がバックエンドAPI経由で読み書きする（`PUT /api/users/{userId}/settings`、`POST /api/users/{userId}/programs/latest/regenerate`）
- APIの形状は[docs/api-contract.yaml](../../../docs/api-contract.yaml)を参照
