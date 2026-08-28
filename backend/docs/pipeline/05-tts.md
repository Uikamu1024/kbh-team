# ⑥ 音声化
**対応ファイル**: `/backend/internal/pipeline/tts.go`（プロバイダ実装は `/backend/internal/providers/tts/`）

## 概要
台本をチャプターごとに音声化する。

## 詳細
- `/backend/internal/providers/tts`配下のプロバイダ実装（デフォルト：`providers/tts/voicevox.go`、VOICEVOX）にTTS呼び出し部分を委譲する。VOICEVOXが使えない場合は同じインターフェースを満たす別プロバイダファイル（例：Google Cloud TTS）を追加すれば`tts.go`側は変更不要
- チャプターごとにTTS APIへ投げ、2話者分の音声を生成・結合
- VOICEVOX ENGINEの出力はWAV（PCM）。MVPではMP3等への変換は行わず、WAVのまま`/backend/data/audio`へ保存する（[⑦保存](./06-storage.md#音声ファイルの保存先)参照）。台詞単位で発話ごとに返るWAVを1チャプター分に結合する処理が必要（`tts.go`側の責務。Goの標準ライブラリにWAV結合を直接行うものはないため、`encoding/binary`でWAVヘッダを読み書きするか、軽量なWAV操作ライブラリの採用を検討する）
- 結合後の音声長（秒）を計測し、`chapters.duration_sec`として保存する（[⑦保存](./06-storage.md)、[docs/api-contract.md](../../../docs/api-contract.md)の`durationSec`に対応）
- 早口すぎない速度に調整（デモでの聞き取りやすさ重視）

## 関連
- 前のステップ：[⑤要約・台本化](./04-script.md)
- 次のステップ：[⑦保存](./06-storage.md)
- VOICEVOX ENGINEはDockerでローカル起動する（クラウドへはデプロイしない。[docker-compose.yml](../../../Directory%20structure.md)参照）
