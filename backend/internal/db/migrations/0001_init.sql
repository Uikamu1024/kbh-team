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
