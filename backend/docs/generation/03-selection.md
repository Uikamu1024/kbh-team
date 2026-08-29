# 生成時の記事選択（キャッシュSELECT、タグベース）

**想定パス**: `/backend/internal/pipeline/select.go`

**改訂(2026-08-29)**：LLMによる重要度スコアリング（生成時・ingest時どちらも）を廃止し、**ユーザーが選択しているタグ一覧に基づく選択**のみに変更する。理由：スコアリング形式は「毎回LLMを呼ぶのは無駄」「ingest時に1回だけ計算する案」など複雑化する一方で、そもそもこのアプリの核となる体験は「タグを選んだらそのタグの記事が届く」ことであり、重要度による並べ替えは必須要件ではないと整理し直した。

## 概要
生成リクエスト（`internal/api`の`demo.go`・`regenerate.go`・`batch.go`ハンドラ）が呼ばれた時点で、外部通信・LLM呼び出しは一切せず、[02-ingestion.md](02-ingestion.md)が事前にキャッシュした`articles`テーブルから、ユーザーのタグに合う記事をSELECTするだけの処理。

## 入力
- `userTags: []string`
- `userID: uuid`（既読トピック除外用）
- `lengthMinutes: int`

## 出力
`domain.Topic`（`Primary`・`RelatedCount`・`TopicGroupID`。重要度スコアは持たない）のスライス、および`changeCount`。

## 選択ロジック

### 1. 候補の絞り込み
```sql
SELECT a.topic_group_id::text, a.title, a.body, a.published_at,
       a.source_name, a.source_url, g.related_count
FROM articles a
JOIN (
  SELECT topic_group_id, count(*) AS related_count
  FROM articles
  GROUP BY topic_group_id
) g ON g.topic_group_id = a.topic_group_id
WHERE a.is_primary = true
  AND a.tags && $1::text[]
  AND a.published_at >= now() - interval '14 days'
  AND NOT EXISTS (
    SELECT 1 FROM user_seen_topics ust
    WHERE ust.user_id = $2::uuid AND ust.topic_group_id = a.topic_group_id
  )
ORDER BY a.published_at DESC
LIMIT 30
```
スコアが無くなったため、**公開日時の新しい順**を唯一の並び順とする。

### 2. 採用件数の決定（変更なし）
`assumedChapterSeconds`（1チャプターあたりの想定尺、実測ベースの固定値）を使い、`ceil(lengthMinutes * 60 / assumedChapterSeconds)`件を上限として、上記1.の結果（既に新しい順）から先頭を採用する。候補がそれに満たない場合は候補全件を採用する。

### 3. `IsNew`・`changeCount`の判定（変更なし）
選ばれたトピックのタイトルを、ユーザーの前回番組の`Chapter.title`一覧（`previousTopics`）とローカルで文字列比較するだけ（LLM不要）。一致しなければ`IsNew: true`。

### 4. 既読記録の反映タイミング（変更なし）
採用した`TopicGroupID`を、番組保存と同じトランザクションで`user_seen_topics`にINSERT（`demo/generate`は保存しないので既読記録もしない）。

## 候補が0件になるケース（変更なし）
`ARTICLE_CACHE_EMPTY`（タグ一致がそもそも無い）／`NO_UNSEEN_ARTICLES`（あるが全部既読）の2種類を区別し、`demo/generate`・`regenerate`はエラー応答、`batch/run`は該当ユーザーをスキップ、という既存方針は変更しない。

## 台本生成（1トピック=1LLM呼び出し）
[04-script.md](../pipeline/04-script.md)で扱う内容だが、選択ロジックと密接なので触れておく。選ばれた各トピックについて、**LLMへの台本生成呼び出しは1トピック=1回**（挨拶文だけ別途1回）。以前の「選定した全トピックをまとめて1回のプロンプトに詰め込み、1回のレスポンスで全チャプター分を返させる」方式は、チャプター数が増えるとプロンプト・レスポンスが肥大化して途中で応答が切れる不具合が実機で確認されたため廃止した。1トピックごとの呼び出しなら、記事本文を切り詰めずに渡しても（[02-ingestion.md](02-ingestion.md)の`abbreviatedBody`を優先的に使う）プロンプトサイズが他のトピックの影響を受けない。

プロンプトには記事のメタデータ（`shortenedTitle`・`tags`・`author`・`sourceName`・`sourceURL`・`publishedAt`）も含める。

## 関連
- 前のステップ：[02-ingestion.md](02-ingestion.md)
- 後続処理：[04-script.md](../pipeline/04-script.md)（台本生成）→ `tts.go`（⑥音声化、変更なし）→ [⑦保存](../pipeline/06-storage.md)
- テーブル定義：[04-schema.md](04-schema.md)
