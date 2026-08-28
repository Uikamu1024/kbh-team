# ③ 重複除去
**対応ファイル**: `/backend/internal/pipeline/dedupe.go`

## 概要
複数ソースで同じ話題が重複取得されるのを防ぎ、同一トピックを1本にまとめる。

## 入力
- `articles: []Article`（[①②収集・正規化](./01-fetch.md)の出力）

## 出力
- `topics: []Topic`
  ```go
  type Topic struct {
      Primary       Article    // 代表記事（本文が最も充実しているものを採用）
      RelatedCount  int        // 同一トピックとみなされた記事の総数（Primary自身を含む。複数ソースでの言及数）
  }
  ```
- `RelatedCount`は[④重要度判定](./03-score.md)の判定材料としてそのまま使う。関連記事の本文自体は捨ててよい（`Primary`のみ後段に渡す）

## 詳細
- タイトルの文字列類似度（or embeddingのコサイン類似度）が閾値以上の`Article`を同一トピックとみなし1本の`Topic`に集約する
- 1件しか見つからなかった記事も`RelatedCount: 1`の`Topic`としてそのまま出力する（除外しない。除外判定は[④重要度判定](./03-score.md)の責務）

## 関連
- 前のステップ：[①②収集・正規化](./01-fetch.md)
- 次のステップ：[④重要度判定](./03-score.md)
