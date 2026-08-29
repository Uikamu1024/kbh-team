# 記事収集リアーキテクチャ設計（案）

**ステータス**: 設計案・未実装（査読待ち）。実装前に必ずレビューを受けること。

## 背景・課題
現行の`internal/providers/fetcher/jina.go`は、タグ名をキーにした固定`map[string][]string`（`testWhitelist`）をコードにハードコードしており、しかも中身はダミーURLのままになっている。この方式には構造的な問題がある：

- 記事の存在をコードで決め打ちしており、実際の最新ニュースを反映しない
- 生成リクエスト（`demo/generate`・`regenerate`・`batch/run`）のたびに外部サイトへライブfetchするため、jina.ai Readerの無料枠レート制限（キーなしは20 RPM）に生成タイミングが直接晒される
- タグを増やす・サイトを差し替えるのにコード変更（再ビルド）が必要

## 新しい設計の方針
「収集」と「生成」を完全に分離する。

```
[収集ジョブ]（定期実行、ユーザーからは独立）
  RSSフィード一覧を読む → 各フィードの記事URL一覧を取得 → 本文取得
  → LLM構造化出力でメタデータ抽出（タグ・省略版タイトル・著者・省略版本文）
  → LLMベースの重複判定 → articlesキャッシュ（PostgreSQL）にupsert

[生成](demo/generate・regenerate・batch/run)
  ユーザーのtags → articlesキャッシュをSELECT（外部通信・LLM呼び出しなし、公開日時の新しい順）
  → 選ばれた各トピックについて1トピック=1回のLLM呼び出しで台本生成（+挨拶文1回）
  → 音声化パイプラインへ
```

**改訂(2026-08-29)**：初稿ではタグ付けをフィード決め打ち・重複判定を文字列類似度・記事の選択をLLM重要度スコアリングで行う設計だったが、ユーザーからのレビューで全面的に見直した。詳細は[3回目の改訂](#3回目の改訂タグ付け重複判定選択方式の全面変更)を参照。

収集ジョブが外部通信（RSS取得・jina.ai本文取得）を一手に引き受けるので、生成側は完全にDBローカルな処理になり、レート制限・レイテンシの両方から解放される。

## ドキュメント構成
1. [01-feed-config.md](01-feed-config.md) — フィード一覧の設定ファイル（`article.json`）の仕様
2. [02-ingestion.md](02-ingestion.md) — 収集ジョブ（RSS取得→タグ付け→重複判定→キャッシュ保存）
3. [03-selection.md](03-selection.md) — 生成時の記事選択（キャッシュからのSELECTと、既存パイプラインへの接続）
4. [04-schema.md](04-schema.md) — 追加するPostgreSQLスキーマ

## 既存ドキュメント・実装との関係
この設計は`docs/pipeline/01-fetch.md`（①②収集・正規化）と`docs/pipeline/02-dedupe.md`（③重複除去）が担っていた責務を置き換える：

| 既存 | 置き換え後 |
| --- | --- |
| `internal/pipeline/fetch.go`（生成リクエスト内でライブfetch） | 廃止。生成時は[03-selection.md](03-selection.md)のキャッシュSELECTに置き換わる |
| `internal/pipeline/dedupe.go`（文字bigram Jaccard類似度） | **廃止（2026-08-29改訂）**。収集ジョブ側（[02-ingestion.md](02-ingestion.md)手順6）でLLMベースの重複判定に置き換え |
| `internal/providers/fetcher/jina.go`の`testWhitelist` | [01-feed-config.md](01-feed-config.md)の`article.json`（RSS URL一覧）に置き換え |
| `internal/pipeline/score.go`（④重要度判定、LLMスコアリング） | **キャッシュ経由の生成では廃止（2026-08-29改訂）**。[03-selection.md](03-selection.md)のタグ・鮮度ベース選択に置き換え。`cmd/demo`のライブfetch経路でのみ引き続き使用 |
| `internal/pipeline/script.go`（台本生成） | **呼び出し方式を変更（2026-08-29改訂）**。全トピックまとめて1回→挨拶文1回＋トピックごとに1回（[docs/pipeline/04-script.md](../pipeline/04-script.md)） |
| `tts.go`（⑥音声化） | 変更なし |

承認され次第、`docs/pipeline/01-fetch.md`・`02-dedupe.md`はこのディレクトリの内容を正として更新する。

## 未決定のまま残す事項（意図的にスコープ外）
- キャッシュの有効期限・削除ポリシー（今回は考えない。記事は貯まる一方で問題ない前提。ただし「選択対象として何日以内の記事を使うか」という**鮮度**の基準は14日で確定済み。削除するかどうかとは別の話）
- ホワイトリスト対象サイト（RSS URL）の最終リスト自体（[01-feed-config.md](01-feed-config.md)は仕組みだけ定義し、実際のURLは別途決める）

## Codexとの相互レビューで確定した論点
初稿に対して`mcp__codex__codex`（gpt-5.6-terra、read-onlyでコードベースを閲覧させて批判的レビューを依頼）から指摘を受け、実コードで裏取りした上で各ファイルへ反映済み。主な変更点：

| 論点 | 結論 | 反映先 |
| --- | --- | --- |
| `0002_article_cache.sql`は`db.go`が読み込まない | 既存`0001_init.sql`への追記に変更 | [04-schema.md](04-schema.md) |
| `domain.Topic`に`topic_group_id`が無く既読記録が実装不能 | `Topic`へ`TopicGroupID`フィールドを追加（`score.go`等は無改修） | [03-selection.md](03-selection.md) |
| 冷スタート（キャッシュ0件）時の挙動が未確定だった | `ARTICLE_CACHE_EMPTY`（503）で明示的に止める。モックへのフォールバックは採用しない | [03-selection.md](03-selection.md) |
| 収集ジョブの並行実行時の重複判定レース | `cmd/ingest`は単一プロセス・逐次処理＋advisory lockで多重起動防止 | [02-ingestion.md](02-ingestion.md) |
| RSSメタデータ vs 現行`jina.go`のメタデータ抽出ロジックの矛盾 | title/pubDate/sourceNameはRSS優先、jina.aiは本文取得のみに使う | [02-ingestion.md](02-ingestion.md) |
| 完全一致URLをスキップすると他フィードのタグを取りこぼす | 本文再取得はスキップするが、タグのUNIONだけは行う | [02-ingestion.md](02-ingestion.md) |
| `RelatedCount`が常に1になるSQLバグ | `topic_group_id`ごとのCOUNTをJOINして取得するSQLに修正 | [03-selection.md](03-selection.md) |
| 重複比較・選択候補の鮮度ウィンドウが「実装者の裁量」のまま | 14日で確定（削除ポリシーではなく選択対象としての鮮度） | [02-ingestion.md](02-ingestion.md)・[03-selection.md](03-selection.md) |
| `is_primary`が各groupに1件である保証が無い | 部分ユニークインデックスをDBレベルで追加 | [04-schema.md](04-schema.md) |
| タグの表記揺れ（`AI`/`ai`等）で検索がヒットしない | フロントの固定タグ一覧と1文字違わず一致させる運用ルールを明記 | [01-feed-config.md](01-feed-config.md) |
| `cmd/demo`・`cmd/gentrace`もキャッシュ経由にすべきか | 意図的に対象外とし、ライブfetchのまま残す（`gentrace`は実通信の検証が目的のため） | [03-selection.md](03-selection.md) |
| `docs/api-contract.yaml`との整合 | 生成時エラーの前提が変わる（`UPSTREAM_FETCH_FAILED`→`ARTICLE_CACHE_EMPTY`/`NO_UNSEEN_ARTICLES`）。**反映済み**（フロントエンド担当への共有は別途必要） | [03-selection.md](03-selection.md)、[docs/api-contract.yaml](../../../docs/api-contract.yaml) |

**Codexの指摘のうち採用しなかったもの**：「既に確立した2つの`topic_group_id`が事後的に統合される場合、`user_seen_topics`の整合性が壊れる」という懸念については、[02-ingestion.md](02-ingestion.md)の重複判定アルゴリズムが新規記事を既存groupへ追記するだけで、確立済みgroup同士を統合する処理が設計上存在しないことを確認し、該当しないと判断した（[04-schema.md](04-schema.md)の`user_seen_topics`節に根拠を明記）。

## 2回目の議論：タグ付け手法・見落とし機能の検討
実装可否のチェックだけでなく、「タグ付けをどう実現すべきか」「他に必要な機能はないか」をCodex（gpt-5.6-terra）と議論し、ユーザーに製品仕様レベルの判断を仰いだ上で確定した：

| 論点 | 結論 | 反映先 |
| --- | --- | --- |
| タグ付け手法（フィード決め打ち／LLM分類／Embedding分類／キーワード分類） | フィード単位の決め打ちのみ採用。LLM・Embedding分類は不採用（`Requirements.md`の「カテゴリマッチング」方針、タグ集合が小さく固定という制約に基づく）。キーワード簡易判定(`includeKeywords`)は将来の拡張ポイントとして明記するに留め、今回は実装しない | [01-feed-config.md](01-feed-config.md) |
| 「該当タグの記事はキャッシュにあるが全部既読」の挙動が未定義だった | `ARTICLE_CACHE_EMPTY`（真に空）と`NO_UNSEEN_ARTICLES`（既読のみ残存）を区別する専用エラーコードを新設。`demo/generate`・`regenerate`はエラー応答、`batch/run`は該当ユーザーだけスキップしてループ継続 | [03-selection.md](03-selection.md) |
| タグUNIONが代表記事（`is_primary`行）に伝播しない実装漏れ | 非代表記事のタグを更新した際、そのgroupの代表記事にも同じUNIONを反映するよう明記 | [02-ingestion.md](02-ingestion.md) |
| 重複判定の比較対象に鮮度フィルタが効いていなかった（既存キャッシュは削除されないため） | 比較クエリ自体に`published_at >= now() - interval '14 days'`を明示的に追加 | [02-ingestion.md](02-ingestion.md) |
| 収集ジョブと生成の実行順序保証 | `cmd/ingest`が最低1回成功していることを前提とする運用注意を明記（cronの間隔をずらす、デモ前は手動実行） | [02-ingestion.md](02-ingestion.md) |
| Firecrawlフォールバック・言語判定・canonical URL・利用規約配慮・極端に短い本文の除外 | いずれも「今回は導入しない／運用ルールとして一言明記するのみ」で確定 | [02-ingestion.md](02-ingestion.md) |
| 最低限の可観測性 | 監視ダッシュボードは作らず、実行ごとにフィード別の取得件数・成功失敗数をログ出力する方針のみ明記 | [02-ingestion.md](02-ingestion.md) |

## 3回目の改訂：タグ付け・重複判定・選択方式の全面変更
初稿の実装を実際にOllama Cloud（gpt-oss:20b）で動かして検証した結果、以下の問題が見つかり、ユーザーの指示で設計を全面的に見直した：

- タグがフィード決め打ちのままで、記事の中身に応じた柔軟な判定ができない
- 重複判定の文字bigram Jaccard類似度は既知の限界があり放置されていた
- 記事本文を一律1200文字に切り詰めていたため、記事によっては（ナビゲーションリンクが本文の前に大量に入るサイトで）本文が実質ゼロになっていた
- 台本生成が「選定した全トピックをまとめて1回のプロンプトで生成」する方式で、チャプール数が増える（14件等）と応答JSONが完成前に打ち切られ、失敗するようになった
- 生成のたびにLLMで重要度スコアリングをやり直すのは無駄

**ユーザーとの質疑応答で確定した内容**：

| 論点 | 決定事項 |
| --- | --- |
| タグ判定の方式 | フィード決め打ちをやめ、**記事の中身をLLMに読ませて判定**する方式に変更（[01-feed-config.md](01-feed-config.md)・[02-ingestion.md](02-ingestion.md)） |
| 構造化出力の採用可否 | Ollama Cloudに構造化出力対応モデル（Gemma系）があるため採用可能。メタデータ抽出はJSON Schemaで型を強制する構造化出力を使う |
| `shortenedTitle`の用途・長さ | UI表示用。45文字以内 |
| 本文の要約(`abbreviatedBody`)の閾値・方式 | 元本文が**2500文字を超える場合のみ**LLMに要約させる。2500文字以下はそのまま元本文を使う（一律切り詰めの廃止） |
| メタデータに追加したい項目 | 公開日時（本文から確認できれば優先）・著者（取得できれば） |
| 重複判定の方式 | タイトル一覧（元タイトル、shortenedTitleではない）をLLMに一括で投げ、同一と思われるものに同じgroup idを割り振る。ingest実行1回につき1回のLLM呼び出し（[02-ingestion.md](02-ingestion.md)手順6） |
| 記事選択の方式 | LLM重要度スコアリングを廃止し、**ユーザーが選択しているタグ一覧＋公開日時の新しい順**で選択（[03-selection.md](03-selection.md)） |
| 台本生成の呼び出し方式 | 選ばれた記事それぞれを**1トピック=1回のLLM呼び出し**で台本化（メタデータをプロンプトに含める）。生成後の読み上げ(TTS)ロジックは変更なし（[docs/pipeline/04-script.md](../pipeline/04-script.md)） |

**反映先ファイル**：[01-feed-config.md](01-feed-config.md)・[02-ingestion.md](02-ingestion.md)・[03-selection.md](03-selection.md)・[04-schema.md](04-schema.md)・[docs/pipeline/03-score.md](../pipeline/03-score.md)（`cmd/demo`専用に縮小）・[docs/pipeline/04-script.md](../pipeline/04-script.md)。

**未確定のまま残る点**：
- 構造化出力に使う具体的なモデル（Ollama Cloud上のGemma系モデル名）は未確定
- LLMベースの重複判定バッチが大きくなりすぎた場合の分割方法は実装時の裁量
- `importance_score`カラム・関連コードの実際の削除（このドキュメント更新時点ではコード側は未着手。前回のセッションで一度実装した`ScoreAndSelect`によるキャッシュ経由スコアリング・ingest時スコアリングは、この改訂により丸ごと不要になる）
