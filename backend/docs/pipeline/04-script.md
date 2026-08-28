# ⑤ 要約・台本化
**対応ファイル**: `/backend/internal/pipeline/script.go`（プロバイダ実装は `/backend/internal/providers/llm/`）

## 概要
LLMに記事群を渡し、口語体のラジオ台本を生成する。

## 詳細
- `/backend/internal/providers/llm`配下のプロバイダ実装（デフォルト：`providers/llm/gemini.go`、Gemini API）にLLM呼び出し部分を委譲する。プロバイダ変更時は`types.go`のインターフェースを満たす新規ファイルを追加するのみで、`script.go`側は変更不要
- LLMに記事群を渡し、口語体のラジオ台本を生成
- 2人の話者による会話形式を採用（単調さ対策）

## プロンプト設計の骨子
- 冒頭に挨拶＋前日との差分言及
- 各トピックへの自然な繋ぎ
- 記事1本＝1チャプターとして区切って出力（後でUIのシークバー・記事リンク展開と対応させるため。[プレイヤー](../../../frontend/docs/features/player.md)参照）
- 末尾に締めの一言

## 関連
- 前のステップ：[④重要度判定](./03-score.md)
- 次のステップ：[⑥音声化](./05-tts.md)
