# フィード設定ファイル（`article.json`）

**想定パス**: `/backend/config/article.json`

**改訂(2026-08-29)**：タグをフィード単位で決め打ちする方式をやめ、記事の中身をLLMに読ませてタグ判定させる方式に変更した（詳細は[02-ingestion.md](02-ingestion.md)）。これに伴い、フィード設定から`tags`フィールドを削除し、収集元の情報だけを持つ最小限の形に単純化する。

## スキーマ

```json
{
  "feeds": [
    {
      "id": "itmedia-aiplus",
      "url": "https://rss.itmedia.co.jp/rss/2.0/aiplus.xml",
      "origin": "itmedia.co.jp",
      "title": "ITmedia AI+"
    },
    {
      "id": "4gamer",
      "url": "https://www.4gamer.net/rss/index.xml",
      "origin": "4gamer.net",
      "title": "4Gamer.net"
    }
  ]
}
```

| フィールド | 説明 |
| --- | --- |
| `id` | フィードの一意な識別子（人間が読めるslug。DBの`articles.feed_id`に記録し、障害調査時にどのフィードが原因か追えるようにする） |
| `url` | RSSフィードのURL |
| `origin` | 発行元サイトのドメイン等の識別子（同じサイトが複数フィードを持つ場合にグルーピングできるようにする。例：`itmedia.co.jp`配下に`aiplus`・`news`等複数フィードがある） |
| `title` | そのフィード自体の表示名（RSSの`<channel><title>`相当。記事の`source_name`に使う） |

`enabled`フィールドは残す（一時的にフィードを外す用途、既存のまま）。

## タグは記事ごとにLLMが判定する（フィード設定からは削除）

以前の設計（フィード単位の決め打ち）は「1フィードが複数カテゴリの記事を混在させる」ケースを扱えない、かつタグ精査の手間がフィード追加のたびに発生するという弱点があった。[02-ingestion.md](02-ingestion.md)で説明する通り、記事の本文を取得した際にLLMへ構造化出力(structured output)で問い合わせ、記事の中身から直接タグを判定させる方式に変更する。

### タグの正規化（変更なし、重要度は上がる）
LLMが判定するタグは、**フロントエンドの固定タグ一覧（オンボーディングのプリセット）と1文字違わず一致する値**に限定する。実装時はLLMへの構造化出力スキーマの`tags`フィールドを、この固定一覧に対する`enum`配列として制約すること（自由記述にしない。表記揺れ・存在しないタグの捏造を防ぐため）。フロントエンド担当への一覧確認が必要な点は従来通り。

## 関連
- 次のステップ：[02-ingestion.md](02-ingestion.md)
