# ⑥ 音声化
**対応ファイル**: `/backend/internal/pipeline/tts.go`（プロバイダ実装は `/backend/internal/providers/tts/`）

## 概要
台本をチャプターごとに音声化する。

## 入力
- `greetingText: string`（[⑤要約・台本化](./04-script.md)の出力）
- `chapters: []ChapterDraft`（同上、各`Lines []Line`を持つ）

## 出力
- `chapters: []ChapterAudio`
  ```go
  type ChapterAudio struct {
      ChapterDraft
      AudioBytes  []byte // WAV（PCM）
      DurationSec int
  }
  ```

## 詳細
- `/backend/internal/providers/tts`配下のプロバイダ実装（デフォルト：`providers/tts/voicevox.go`、VOICEVOX）にTTS呼び出し部分を委譲する。VOICEVOXが使えない場合は同じインターフェースを満たす別プロバイダファイル（例：Google Cloud TTS）を追加すれば`tts.go`側は変更不要
- `Line.Speaker`（`"A"` / `"B"`）をVOICEVOXのキャラクターID（`speaker`パラメータ）にマッピングする対応表を`providers/tts/voicevox.go`内に持つ
- `chapters[0]`（`Position: 0`）は、`Lines`の先頭に`greetingText`を話者Aのセリフとして追加してから合成する（挨拶文専用の音声ファイルは作らない。[⑤要約・台本化](./04-script.md#greetingtextの生成)参照）
- 各`Line`ごとにVOICEVOX ENGINEへリクエストしてWAVを取得し、同一チャプター内の`Line`のWAVを結合して1チャプター分の`AudioBytes`にする。VOICEVOX ENGINEの出力はWAV（PCM）。MVPではMP3等への変換は行わず、WAVのまま保存する（[⑦保存](./06-storage.md#音声ファイルの保存先)参照）。Goの標準ライブラリにWAV結合を直接行うものはないため、`encoding/binary`でWAVヘッダを読み書きするか、軽量なWAV操作ライブラリの採用を検討する
- 結合後の音声長（秒）を計測し`DurationSec`に設定する（[⑦保存](./06-storage.md)で`chapters.duration_sec`として保存、[docs/api-contract.yaml](../../../docs/api-contract.yaml)の`durationSec`に対応）
- 早口すぎない速度に調整（デモでの聞き取りやすさ重視）

## 関連
- 前のステップ：[⑤要約・台本化](./04-script.md)
- 次のステップ：[⑦保存](./06-storage.md)
- VOICEVOX ENGINEはDockerでローカル起動する（クラウドへはデプロイしない。[docker-compose.yml](../../../Directory%20structure.md)参照）
