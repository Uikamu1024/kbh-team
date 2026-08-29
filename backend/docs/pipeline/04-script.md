# ⑤ 要約・台本化
**対応ファイル**: `/backend/internal/pipeline/script.go`（プロバイダ実装は `/backend/internal/providers/llm/`）

**改訂(2026-08-29)**：キャッシュ経由の生成（`demo/generate`・`regenerate`・`batch/run`）では重要度スコアリングを廃止し、選択は[docs/generation/03-selection.md](../generation/03-selection.md)のタグ・鮮度ベースに変更した。それに伴い、台本生成は**選定した全トピックをまとめて1回のプロンプトで生成する方式をやめ、挨拶文1回＋トピックごとに1回、という1トピック=1LLM呼び出し方式に変更した**。

理由：全トピックまとめ方式は、選定件数が増える（例：14件）とプロンプト・レスポンスが肥大化し、LLMの応答JSONが完成する前に打ち切られる不具合が実機で確認された。1トピック=1呼び出しなら、記事本文を切り詰めずに渡しても（[docs/generation/02-ingestion.md](../generation/02-ingestion.md)の`abbreviatedBody`があればそちらを優先、無ければ元の本文をそのまま）他トピックの影響を受けず、失敗時のリトライも1トピック分だけで済む。

`cmd/demo`（ライブfetchのCLIデモ）は本改訂の対象外で、引き続き[④重要度判定](./03-score.md)によるLLMスコアリング＋従来方式のままでよい。

## 入力
- `selected: []ScoredTopic`（キャッシュ経由の場合は[docs/generation/03-selection.md](../generation/03-selection.md)の出力、公開日時降順の`Position`順。`cmd/demo`のライブfetch経路の場合は[④重要度判定](./03-score.md)の出力）

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
- **挨拶文生成呼び出し（1回）**：差分件数(`changeCount`)への言及のみを依頼する小さいプロンプト。記事内容は含めない
- **チャプター生成呼び出し（トピックごとに1回）**：そのトピックの記事メタデータ（`shortenedTitle`・`tags`・`author`・`sourceName`・`sourceURL`・`publishedAt`）と本文（`abbreviatedBody`優先）を渡し、自然な繋ぎのセリフを含む2〜4行の会話を生成させる。最後のトピックの呼び出しにのみ、番組全体の締めの一言を含めるよう指示する
- 両方ともLLMのJSON応答パース失敗時は1〜2回リトライする（推論モデルが出力を書き切れず途中で切れることがあるため）

## 関連
- 前のステップ：[④重要度判定](./03-score.md)
- 次のステップ：[⑥音声化](./05-tts.md)
