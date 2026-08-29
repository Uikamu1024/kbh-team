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

1. スコア降順に並んだ`Topic`のうち、上位`ceil(lengthMinutes * 60 / assumedChapterSeconds)`件を採用する（`assumedChapterSeconds`は1チャプターあたりの想定音声長。実測値に基づく固定値、`score.go`参照）
2. 候補が採用予定件数に満たない場合は、候補全件を採用する（0チャプターの番組を作らない）
3. 採用した`Topic`に`Position`（0始まり、スコア降順のまま）を振って`ScoredTopic`にする

**修正履歴(2026-08-29)**：以前は各記事の`Primary.Body`（jina.aiが取得した生Markdown、ナビゲーション等のノイズ込み）の文字数から推定尺を計算し累積していたが、これは誤りだった。実際に読み上げられるのは[⑤要約・台本化](./04-script.md)がLLMで生成する短い会話台本（2〜4行）であり、元記事の生本文の長さとは無関係。生本文は数千〜数万文字あるため、この方式だと1記事だけで`lengthMinutes`の予算を超過し、常に1チャプターしか採用されない不具合が実機で確認された（結果、`lengthMinutes=10`を指定しても実際の番組が1分未満になっていた）。固定の想定尺に基づく方式に変更した。

## 出力への反映
- `Position: 0`（＝スコア最上位）のトピックが番組冒頭に配置され、「今日はこれが一番動いています」の一言が[⑤要約・台本化](./04-script.md)で挿入される
- 採用されなかった`Topic`（尺予算を超えた分）は番組から完全に除外される（末尾に回す等はしない。質の低い記事の除外もこの足切りで兼ねる）

## 関連
- 前のステップ：[③重複除去](./02-dedupe.md)
- 次のステップ：[⑤要約・台本化](./04-script.md)
