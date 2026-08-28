# ⑥ 音声化
**対応ファイル**: `/backend/internal/pipeline/tts.go`（プロバイダ実装は `/backend/internal/providers/tts/`）

## 概要
台本をチャプターごとに音声化する。

## 詳細
- `/backend/internal/providers/tts`配下のプロバイダ実装（デフォルト：`providers/tts/voicevox.go`、VOICEVOX）にTTS呼び出し部分を委譲する。VOICEVOXが使えない場合は同じインターフェースを満たす別プロバイダファイル（例：Google Cloud TTS）を追加すれば`tts.go`側は変更不要
- チャプターごとにTTS APIへ投げ、2話者分の音声を生成・結合
- 早口すぎない速度に調整（デモでの聞き取りやすさ重視）

## 関連
- 前のステップ：[⑤要約・台本化](./04-script.md)
- 次のステップ：[⑦保存](./06-storage.md)
- VOICEVOX ENGINEはDockerでローカル起動する（クラウドへはデプロイしない。[docker-compose.yml](../../Directory%20structure.md)参照）
