# 生成時の記事選択（キャッシュSELECT）

**想定パス**: `/backend/internal/pipeline/select.go`（新規。既存の`fetch.go`を置き換える）

## 概要
生成リクエスト（**`internal/api`の`demo.go`・`regenerate.go`・`batch.go`ハンドラ**）が呼ばれた時点で、外部通信は一切せず、[02-ingestion.md](02-ingestion.md)が事前にキャッシュした`articles`テーブルから、ユーザーのタグに合う記事グループをSELECTするだけの処理。これが現行の`internal/pipeline/fetch.go`（`FetchArticles`）と`dedupe.go`（`DedupeArticles`）の呼び出しを置き換える。

**適用範囲の明確化（Codexレビューを受けて追記）**：この置き換えはHTTPハンドラ経由の生成のみが対象。`cmd/demo`（Phase 1のCLIデモ）と`cmd/gentrace`（外部プロバイダへの実通信をトレースして確認するデバッグ専用ツール）は、**意図的にライブfetch（`FetchArticles`+`DedupeArticles`）のまま残す**。理由：`gentrace`はそもそも「プロバイダが実際に動くか」を確認するための道具であり、キャッシュ経由にすると存在意義が無くなる。`cmd/demo`も同様に手動検証用として現状維持でよい。

## 入力
- `userTags: []string`（ユーザーが選択したテーマタグ、`users.tags`）
- `userID: uuid`（後述の「既読トピック」除外に使う）

## 出力
既存の`domain.Topic`に**フィールドを1つ追加する**（Codexレビューで判明：現行の`Topic`には`topic_group_id`を運ぶ手段が無く、「既読記録の反映」（手順4）が実装できないため、最小限の型変更が必要）：
```go
type Topic struct {
    Primary      Article
    RelatedCount int
    TopicGroupID uuid.UUID // 追加。articles.topic_group_idをそのまま保持する
}
```
`ScoredTopic`・`ChapterDraft`・`ChapterAudio`はいずれも`Topic`を埋め込んでいる（`internal/domain/types.go`）ため、このフィールドは何もしなくても後段まで自動的に伝播する。`score.go`・`script.go`・`tts.go`のロジック自体（`TopicGroupID`を参照しない部分）は無改修のままでよい。

- `Primary`：`articles`テーブルの`is_primary = true`行から詰め直す
- `RelatedCount`：同じ`topic_group_id`を持つ行の件数（[02-ingestion.md](02-ingestion.md)で重複統合された記事数）。算出方法は下記「候補の絞り込み」を参照（素朴な`is_primary = true`だけのSELECTでは常に1になってしまう点に注意）
- `TopicGroupID`：`articles.topic_group_id`（[04-schema.md](04-schema.md)）

## 選択ロジック

### 1. 候補の絞り込み
`RelatedCount`（グループの記事数）も同時に取得するため、`is_primary`行に対して`topic_group_id`ごとの件数をJOINする：
```sql
SELECT a.*, g.related_count
FROM articles a
JOIN (
  SELECT topic_group_id, count(*) AS related_count
  FROM articles
  GROUP BY topic_group_id
) g ON g.topic_group_id = a.topic_group_id
WHERE a.is_primary = true
  AND a.tags && $1::text[]                    -- ユーザーのtagsと配列オーバーラップ（1件でも一致すれば対象）
  AND a.published_at >= now() - interval '14 days'  -- 鮮度フィルタ（02-ingestion.mdの基準と統一）
ORDER BY a.published_at DESC
LIMIT 30
```
候補プールの上限（例：30件）は、後続の④重要度判定（LLM採点）の呼び出し回数を抑えるための母数調整。[01-fetch.md](../pipeline/01-fetch.md)で「タグあたり10〜20件」としていた制限に相当する。

### 2. 既読トピックの除外
同じユーザーに同じニュースを繰り返し聞かせないため、`user_seen_topics`（[04-schema.md](04-schema.md)）に記録済みの`topic_group_id`を候補から除外する：
```sql
... AND topic_group_id NOT IN (
  SELECT topic_group_id FROM user_seen_topics WHERE user_id = $2
)
```
- 除外の有効期間は設けない（一度そのユーザーに配信したトピックは、記事キャッシュを削除しない限りずっと除外され続ける想定。将来的にウィンドウを設けたくなったら`seen_at`カラムで絞ればよい）
- この除外は「候補プールに含めるかどうか」のハードな足切りであり、既存の`score.go`にある`previousTopics`による`IsNew`判定（前回番組との差分検知・スコアリングの参考情報）とは役割が異なる。**両方とも残す**：こちらは「二度と同じ話を聞かせない」、`previousTopics`は「直近の番組からの変化量（`changeCount`）を測る」

### 3. `Topic`への変換
残った候補（既に`topic_group_id`単位に集約済み。1つの`topic_group_id`につき`is_primary`行が1件しかないことは[04-schema.md](04-schema.md)のユニークインデックスで保証されている）を`domain.Topic{Primary: 代表記事, RelatedCount: related_count, TopicGroupID: topic_group_id}`に変換する。

### 4. 既読記録の反映タイミング
`ScoreAndSelect`が最終的に採用した（＝実際に番組に入った）トピックの`TopicGroupID`を、`user_seen_topics`にINSERTする。

- **`regenerate`・`batch/run`**：番組をDBに保存するのと同じトランザクションで実施（[⑦保存](../pipeline/06-storage.md)のタイミング）
- **`demo/generate`**：DB書き込みを一切行わない既存方針（[api-handlers.md](../api-handlers.md)参照）を踏襲し、既読記録もしない

## 候補が0件になるケース（ユーザー確認済み・確定）
候補プールが0件になりうるケースは性質の異なる2つがあり、**区別してエラーコードを分ける**（どちらもモックへの自動フォールバックは採用しない。「実在するニュースが読み上げられる」という利用者の期待をハッカソンのデモ中に裏切ることになるため）。放置すると、現行の`ScoreAndSelect`は入力0件で`(nil, 0, nil)`を返すため、そのまま進むと**0チャプターの番組が生成されてしまう**（サイレントに壊れた状態で成功扱いになる、最悪のパターン）ので、いずれの場合もパイプラインをここで明示的に止める。

### ケース1：`ARTICLE_CACHE_EMPTY`（真にキャッシュが空）
「1. 候補の絞り込み」（既読除外を適用する**前**の時点、タグ一致＋鮮度フィルタのみ）で0件の場合。収集ジョブが未実行、または該当タグの記事がまだキャッシュに存在しない状態。

### ケース2：`NO_UNSEEN_ARTICLES`（キャッシュはあるが全部既読）
「1.」の時点では1件以上あるが、「2. 既読トピックの除外」を適用した後に0件になる場合。ユーザーの興味に合う記事はあるが、直近14日以内に新しい話題が増えていない状態（タグごとの記事数が少ないテーマ、例えば「京都」で特に起きやすい）。

### エンドポイントごとの挙動（ユーザー確認済み）
- **`demo/generate`・`regenerate`**：どちらのケースもエラーレスポンスを返す（`ARTICLE_CACHE_EMPTY`または`NO_UNSEEN_ARTICLES`、いずれもHTTP 503）。`regenerate`の場合、この2つのエラーは`reset_count`（1日3回の作り直し上限）を消費させない（パイプライン起動前に弾く既存の`ALREADY_GENERATING`判定と同じ扱い）
- **`batch/run`**：全ユーザーを走査するループの中で、該当ユーザーだけをスキップしてログに記録し、次のユーザーの処理を継続する（バッチ全体を失敗させない）。そのユーザーの`latest program`は前回のものが維持され、更新されない

**反映済み**：[docs/api-contract.yaml](../../../docs/api-contract.yaml)の`demo/generate`・`regenerate`に`503`（`ARTICLE_CACHE_EMPTY`/`NO_UNSEEN_ARTICLES`）を追加し、`UPSTREAM_FETCH_FAILED`は両エンドポイントの説明・共通エラーコード列挙から削除済み（生成時は外部fetchを一切行わなくなったため、このコードはもうどのHTTPエンドポイントからも返らない）。フロントエンド担当（別セッション）への共有が必要。

## 関連
- 前のステップ：[02-ingestion.md](02-ingestion.md)
- 後続処理：`internal/pipeline/score.go`（④重要度判定、無改修）→ `script.go`（⑤）→ `tts.go`（⑥）→ [⑦保存](../pipeline/06-storage.md)
- テーブル定義：[04-schema.md](04-schema.md)
