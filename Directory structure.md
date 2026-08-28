# ディレクトリ構成

開発が進むにつれて階層が変わる可能性が高いため、`CLAUDE.md`とは別ファイルで管理する。変更した場合はこのファイルを更新すること。

```
/app                 Next.js App Router
  /onboarding        テーマ選択画面
  /home              番組準備完了画面
  /player            プレイヤー画面
/lib
  /pipeline          収集→正規化→重複除去→重要度判定→台本化→音声化（オーケストレーション）
    fetch.ts
    dedupe.ts
    score.ts
    script.ts
    tts.ts
  /providers         外部サービス実装（差し替え可能にする層）
    /fetcher
      types.ts        共通インターフェース
      jina.ts          jina.ai Reader実装（デフォルト）
      firecrawl.ts     firecrawl実装（代替）
    /llm
      types.ts        共通インターフェース
      gemini.ts        Gemini実装（デフォルト）
    /tts
      types.ts        共通インターフェース
      voicevox.ts      VOICEVOX実装（デフォルト）
  /firebase          Firestore/Storageクライアント
/public              PWAマニフェスト、アイコン
/scripts             デモ用の軽量パイプライン実行スクリプト
```

`pipeline/*.ts`は`providers/*/types.ts`のインターフェースだけを参照し、実装はプロバイダファイル単位で完結させる。プロバイダを変更する場合は環境変数（例：`TTS_PROVIDER=voicevox`）で切り替え、インターフェースを満たす新しいファイルを1つ追加するだけで済む設計にする（`pipeline`側のコードは変更不要）。
