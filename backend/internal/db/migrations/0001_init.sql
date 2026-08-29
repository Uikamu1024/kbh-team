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
  duration_sec INT NOT NULL,
  importance_score INT NOT NULL
);

CREATE INDEX IF NOT EXISTS programs_user_created_at_idx
  ON programs (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS chapters_program_position_idx
  ON chapters (program_id, position);

CREATE TABLE IF NOT EXISTS articles (
  id UUID PRIMARY KEY,
  feed_id TEXT NOT NULL,
  topic_group_id UUID NOT NULL,
  is_primary BOOLEAN NOT NULL,
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  published_at TIMESTAMPTZ NOT NULL,
  source_name TEXT NOT NULL,
  source_url TEXT NOT NULL UNIQUE,
  tags TEXT[] NOT NULL DEFAULT '{}',
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
