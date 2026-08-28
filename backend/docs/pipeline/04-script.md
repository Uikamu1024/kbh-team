# ⑤ 要約・台本化
**対応ファイル**: `/backend/internal/pipeline/script.go`（プロバイダ実装は `/backend/internal/providers/llm/`）

## 概要
LLMに記事群を渡し、口語体のラジオ台本を生成する。2人の話者（話者A・話者B）による会話形式を採用（単調さ対策）。

## 入力
- `selected: []ScoredTopic`（[④重要度判定](./03-score.md)の出力、`Position`順）

## 出力
- `greetingText: string`（番組冒頭の挨拶文。API応答の`Program.greetingText`に対応）
- `chapters: []ChapterDraft`
  ```go
  type ChapterDraft struct {
      ScoredTopic
      Lines []Line // この記事に対応する読み上げ台本
  }

  type Line struct {
      Speaker string // "A" または "B"
      Text    string
  }
  ```

## 詳細
- `/backend/internal/providers/llm`配下のプロバイダ実装（デフォルト：`providers/llm/gemini.go`、Gemini API）にLLM呼び出し部分を委譲する。プロバイダ変更時は`types.go`のインターフェースを満たす新規ファイルを追加するのみで、`script.go`側は変更不要
- 記事1本（`ScoredTopic`1件）＝1チャプター（`ChapterDraft`1件）として区切って出力する（後でUIのシークバー・記事リンク展開と対応させるため。[プレイヤー](../../../frontend/docs/features/player.md)参照）
- `Chapter.script`としてAPIに公開する文字列は、その`ChapterDraft.Lines`の`Text`を改行区切りで連結したもの（話者情報はAPIには出さない。[docs/api-contract.yaml](../../../docs/api-contract.yaml)の`Chapter.script`参照）

## `greetingText`の生成
- 番組全体で1つだけ生成する、どの`ChapterDraft`にも属さない冒頭の挨拶文
- 「今日は〇〇について3件動きがあります」のように、[④重要度判定](./03-score.md)が返した`changeCount`（差分件数）を言及する内容にする
- ユーザーの表示名（「〇〇さん」）は含めない。表示名はクライアントローカルの値のため、フロントエンドが`greetingText`の前に自分で付け足す（[docs/api-contract.yaml](../../../docs/api-contract.yaml)の「表示名（ニックネーム）の扱い」参照）
- 音声としては、`Position: 0`のチャプター（`chapters[0]`）の音声の冒頭にこの挨拶文を読み上げさせる形で組み込む（挨拶文専用の音声ファイルは作らない。[⑥音声化](./05-tts.md)参照）。あくまで`greetingText`はテキスト表示用の値で、`chapters[0].Lines`とは別に保持する

## プロンプト設計の骨子
- 冒頭に挨拶＋前日との差分言及（`greetingText`として別出力させる）
- 各トピックへの自然な繋ぎ
- 末尾に締めの一言

## 関連
- 前のステップ：[④重要度判定](./03-score.md)
- 次のステップ：[⑥音声化](./05-tts.md)
