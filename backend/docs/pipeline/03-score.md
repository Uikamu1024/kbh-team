# ④ 重要度判定
**対応ファイル**: `/backend/internal/pipeline/score.go`

## 概要
記事の重要度をスコアリングし、番組冒頭に重要トピックを配置する。「何件を番組に採用するか」を`lengthMinutes`（ポッドキャストの長さ設定）に基づいて決めるのもこのステップの責務。質の低い記事の除外も兼ねる（別機能として作らない）。

## 入力
- `topics: []Topic`（[③重複除去](./02-dedupe.md)の出力）
- `lengthMinutes: int`（5・10・15のいずれか。`users.length_minutes`、[プロフィール](../../../frontend/docs/features/home.md)の設定）
- `previousTopics: []string`（前日の番組で扱った`Chapter.title`の一覧。差分検知用。前日分の番組が無ければ空配列）

## 出力
- `selected: []ScoredTopic`（番組に採用する`Topic`。`position`順・重要度降順）
  ```go
  type ScoredTopic struct {
      Topic
      ImportanceScore int  // 1〜5点（LLM採点）
      IsNew           bool // previousTopicsに無かった＝新規/差分トピック
      Position        int  // 番組内の並び順（0が最重要）
  }
  ```
- `changeCount: int`（`selected`のうち`IsNew == true`の件数。API応答の`Program.changeCount`に対応）

## 採点ロジック
1. 各`Topic`をLLMに渡し、重要度スコア（1〜5点）を採点させる。判定材料：
   - `RelatedCount`（複数ソースで同時報道されているか。[③重複除去](./02-dedupe.md)の結果を流用）
   - `previousTopics`との類似度（低ければ新規トピック＝`IsNew: true`）
2. スコア降順で`Topic`を並べる（同点なら`RelatedCount`が多い方を優先）

## `lengthMinutes`による採用件数の決定
記事を取得する件数（10〜20件、[①②収集・正規化](./01-fetch.md)）は固定のままで、このステップで「番組に残す件数」だけを`lengthMinutes`に応じて絞り込む：

1. スコア降順に並んだ`Topic`を先頭から見ていき、各記事の推定尺（目安：日本語の読み上げは概ね300〜350字/分。`Primary.Body`の文字数から概算）を累計する
2. 累計が`lengthMinutes`を超える直前まで採用する
3. 採用できる記事が0件になる場合でも、スコア最上位の1件は必ず採用する（0チャプターの番組を作らない）
4. 採用した`Topic`に`Position`（0始まり、スコア降順のまま）を振って`ScoredTopic`にする

## 出力への反映
- `Position: 0`（＝スコア最上位）のトピックが番組冒頭に配置され、「今日はこれが一番動いています」の一言が[⑤要約・台本化](./04-script.md)で挿入される
- 採用されなかった`Topic`（尺予算を超えた分）は番組から完全に除外される（末尾に回す等はしない。質の低い記事の除外もこの足切りで兼ねる）

## 関連
- 前のステップ：[③重複除去](./02-dedupe.md)
- 次のステップ：[⑤要約・台本化](./04-script.md)
