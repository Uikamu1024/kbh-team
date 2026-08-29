# 追加PostgreSQLスキーマ（案）

既存の[⑦保存](../pipeline/06-storage.md)のスキーマ（`users` / `programs` / `chapters`）に、以下2テーブルを追加する。

## マイグレーションの適用方法（Codexレビューで確定）
`internal/db/db.go`は`//go:embed migrations/0001_init.sql`で**単一ファイルのみ**を読み込み、`Open()`時にそれだけを実行する仕組みになっている。したがって`0002_article_cache.sql`のような新規ファイルを追加しても、`db.go`側で新しいembed宣言を追加しない限り実行されない。

今回は**既存の`0001_init.sql`に追記する**方式を採る（`db.go`の変更を避け、既存の起動フローを一切変えずに済むため）。`CREATE TABLE IF NOT EXISTS`の冪等性は維持する。

## `articles`（記事キャッシュ）

**改訂(2026-08-29)**：`importance_score`を廃止（[03-selection.md](03-selection.md)参照、選択はタグベースに変更したためスコアが不要になった）。LLM構造化出力（[02-ingestion.md](02-ingestion.md)手順5）で取得する`shortened_title`・`author`を追加し、本文は`abbreviatedBody`が得られた場合のみそちらを別カラムに保持する。

```sql
CREATE TABLE IF NOT EXISTS articles (
  id UUID PRIMARY KEY,
  feed_id TEXT NOT NULL,              -- article.json上のフィードid（障害調査用のトレーサビリティ）
  topic_group_id UUID NOT NULL,       -- 重複判定(LLMベース)で同一トピックとみなされた記事群のグループID（自分自身のidの場合もある）
  is_primary BOOLEAN NOT NULL,        -- グループ内で本文が最も充実している代表記事か
  title TEXT NOT NULL,                -- 元タイトル（重複判定・台本生成のソース情報として使う）
  shortened_title TEXT,               -- LLMが生成したUI表示用の短いタイトル（45文字以内、nullを許容）
  author TEXT,                        -- LLMが本文から抽出した著者名（抽出できなければNULL）
  body TEXT NOT NULL,                 -- 元の本文（jina.ai取得のまま）
  abbreviated_body TEXT,              -- 本文が2500文字を超える場合のみLLMが生成した要約。NULLの場合はbodyをそのまま使う
  published_at TIMESTAMPTZ NOT NULL,  -- 本文から確認できればLLM抽出値を優先、できなければRSSのpubDate
  source_name TEXT NOT NULL,
  source_url TEXT NOT NULL UNIQUE,    -- 新着判定（02-ingestion.mdの手順3）に使う
  tags TEXT[] NOT NULL DEFAULT '{}',  -- 記事ごとにLLMが本文から判定。代表記事はグループ内全記事のタグを合算
  fetched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS articles_topic_group_id_idx ON articles (topic_group_id);
CREATE INDEX IF NOT EXISTS articles_tags_gin_idx ON articles USING GIN (tags);

-- 各topic_group_idにつき代表記事(is_primary=true)がちょうど1件であることをDBレベルで保証する。
-- これが無いと、[02-ingestion.md](02-ingestion.md)の代表切替処理にバグがあった場合に
-- 代表0件・複数件の状態がサイレントに発生し、[03-selection.md](03-selection.md)のSELECTが壊れる。
CREATE UNIQUE INDEX IF NOT EXISTS articles_one_primary_per_group_idx
  ON articles (topic_group_id) WHERE is_primary;
```

- `tags && $1::text[]`（配列オーバーラップ検索、[03-selection.md](03-selection.md)）用に`tags`へGINインデックスを張る。ハッカソン規模のデータ量では無くても致命的に遅くはならないが、張ること自体のコストもほぼ無いため残す（「これが無いと遅い」という強い主張はしない）
- `RelatedCount`（同一`topic_group_id`の記事数。重複判定は[02-ingestion.md](02-ingestion.md)手順6のLLMベース判定に変更済み）はテーブルに持たせず都度算出する。ただし[03-selection.md](03-selection.md)の候補取得SQLは`is_primary = true`の行だけを見るため、素朴な`count(*)`をそのまま足すと集計対象が無くなる点に注意（詳細は[03-selection.md](03-selection.md)の該当箇所を参照）
- 削除・有効期限ポリシーは今回設けない（明示的にスコープ外とする）。ただし記事の**選択対象としての鮮度**（何日以内の記事を候補にするか）は別途[02-ingestion.md](02-ingestion.md)・[03-selection.md](03-selection.md)で定める（キャッシュから消すかどうかとは別の話）

## `user_seen_topics`（ユーザーごとの既読トピック）
```sql
CREATE TABLE IF NOT EXISTS user_seen_topics (
  user_id UUID NOT NULL REFERENCES users(id),
  topic_group_id UUID NOT NULL,
  seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, topic_group_id)
);
```

- [03-selection.md](03-selection.md)の「既読トピック除外」に使う
- `topic_group_id`は`articles.topic_group_id`を参照する想定だが、あえて外部キー制約は張らない（`articles`側の行が将来何らかの理由で削除された場合でも、既読記録自体は残ってよいため）

**設計上の前提**：「既に確立した2つの`topic_group_id`が後から統合されると、既読記録の整合性が壊れるのでは」という懸念について。[02-ingestion.md](02-ingestion.md)手順6のLLMベース重複判定は、新規記事を「既存groupへ合流」または「バッチ内の他の新規記事とグルーピングして新group発行」のいずれかにするだけで、**既に確立された2つのgroup同士を事後的に統合する処理は無い**。したがって`topic_group_id`は一度発行されたら他のgroup_idに吸収されることはなく、この懸念は本設計には該当しない。将来groupの事後マージ機能を追加する場合は、本セクションの設計（FK制約なし）を見直すこと。

## 既存テーブルへの変更
なし。`chapters`テーブルは現行のまま（`source_url`等を引き続き非正規化して保持する。`articles`テーブルへのFK付与は行わない＝生成後にキャッシュ側の記事が更新されても、既に生成済みの番組の内容は変わらない）。

## 関連
- 使う場所：[02-ingestion.md](02-ingestion.md)（書き込み）、[03-selection.md](03-selection.md)（読み込み・既読記録）
