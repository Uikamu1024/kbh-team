# ①② 収集・正規化
**対応ファイル**: `/backend/internal/pipeline/fetch.go`（プロバイダ実装は `/backend/internal/providers/fetcher/`）

## 概要
テーマ（タグ）ごとに紐付けたホワイトリストサイト・RSSフィードから記事を取得し、後続処理で使える共通フォーマットに正規化する。

## 収集
- テーマ（タグ）ごとに紐付けたホワイトリストサイト・RSSフィードから記事を取得
- 取得元は`/backend/internal/providers/fetcher`配下のプロバイダ実装を呼び出す。jina.ai Reader（`providers/fetcher/jina.go`、`https://r.jina.ai/<URL>` で本文をMarkdown化して取得）を第一候補とし、詰まった場合は`providers/fetcher/firecrawl.go`（firecrawl free tier）に切替。切替は`types.go`の共通インターフェースを満たす限り`fetch.go`側の変更なしで行える
- 取得件数はテーマあたり10〜20件に制限（レイテンシ・トークン消費対策）

## 正規化
- タイトル・本文（先頭数百字で可）・公開日時・ソース名を共通フォーマットに変換
- 全文取得が失敗するケースを想定し、ディスクリプション/リード文ベースの要約でも成立する設計にする

## 関連
- 記事取得元は最初からホワイトリスト化した数サイトに限定し、全サイト対応は行わない（[backend/CLAUDE.md](../../CLAUDE.md)参照）
- ホワイトリスト対象サイトの最終リストは未決定（[backend/CLAUDE.md](../../CLAUDE.md)の未決定事項参照）
- 次のステップ：[③重複除去](./02-dedupe.md)
