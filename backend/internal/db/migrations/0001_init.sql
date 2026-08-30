CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY,
  tags TEXT[] NOT NULL DEFAULT '{}'::text[],
  delivery_time TIME NOT NULL DEFAULT '06:00',
  length_minutes INT NOT NULL DEFAULT 10,
  reset_count INT NOT NULL DEFAULT 0,
  reset_date DATE
);

CREATE TABLE IF NOT EXISTS programs (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  greeting_text TEXT NOT NULL,
  change_count INT NOT NULL
);

CREATE TABLE IF NOT EXISTS chapters (
  id UUID PRIMARY KEY,
  program_id UUID NOT NULL REFERENCES programs(id),
  position INT NOT NULL,
  title TEXT NOT NULL,
  source_url TEXT NOT NULL,
  source_name TEXT NOT NULL,
  script TEXT NOT NULL,
  audio_path TEXT NOT NULL,
  duration_sec INT NOT NULL
);

CREATE INDEX IF NOT EXISTS programs_user_created_at_idx
  ON programs (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS chapters_program_position_idx
  ON chapters (program_id, position);

CREATE TABLE IF NOT EXISTS articles (
  id UUID PRIMARY KEY,
  feed_id TEXT NOT NULL,              -- rss.json上のフィードid（障害調査用のトレーサビリティ）
  topic_group_id UUID NOT NULL,       -- 重複判定(LLMベース)で同一トピックとみなされた記事群のグループID（自分自身のidの場合もある）
  is_primary BOOLEAN NOT NULL,        -- グループ内で本文が最も充実している代表記事か
  title TEXT NOT NULL,                -- 元タイトル（重複判定・台本生成のソース情報として使う）
  shortened_title TEXT,               -- LLMが生成したUI表示用の短いタイトル（45文字以内、nullを許容）
  author TEXT,                        -- LLMが本文から抽出した著者名（抽出できなければNULL）
  body TEXT NOT NULL,                 -- 元の本文（jina.ai取得のまま）
  abbreviated_body TEXT,              -- 本文が2500文字を超える場合のみLLMが生成した要約。NULLの場合はbodyをそのまま使う
  published_at TIMESTAMPTZ NOT NULL,  -- 本文から確認できればLLM抽出値を優先、できなければRSSのpubDate
  source_name TEXT NOT NULL,
  source_url TEXT NOT NULL UNIQUE,    -- 新着判定に使う
  tags TEXT[] NOT NULL DEFAULT '{}',  -- 記事ごとにLLMが本文から判定。代表記事はグループ内全記事のタグを合算
  fetched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS articles_topic_group_id_idx ON articles (topic_group_id);
CREATE INDEX IF NOT EXISTS articles_tags_gin_idx ON articles USING GIN (tags);
CREATE UNIQUE INDEX IF NOT EXISTS articles_one_primary_per_group_idx
  ON articles (topic_group_id) WHERE is_primary;

CREATE TABLE IF NOT EXISTS user_seen_topics (
  user_id UUID NOT NULL REFERENCES users(id),
  topic_group_id UUID NOT NULL,
  seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, topic_group_id)
);

-- DEMO_MODE=1のときだけ使う。実生成の代わりに既存のprograms行をユーザーへ
-- ランダムに割り当てる（重い生成処理を無効化するデモ用の仕組み）。
-- programs/chaptersなど本番の音声管理テーブルは一切変更しない。
CREATE TABLE IF NOT EXISTS demo_program_assignments (
  user_id UUID NOT NULL REFERENCES users(id),
  program_id UUID NOT NULL REFERENCES programs(id),
  assigned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, program_id)
);

CREATE INDEX IF NOT EXISTS demo_program_assignments_user_assigned_at_idx
  ON demo_program_assignments (user_id, assigned_at DESC);
