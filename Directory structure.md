# ディレクトリ構成

開発が進むにつれて階層が変わる可能性が高いため、`CLAUDE.md`とは別ファイルで管理する。変更した場合はこのファイルを更新すること。

フロントエンド（Next.js）とバックエンド（Go）は別サービスとして分離し、同一リポジトリ内で`/frontend`と`/backend`に分ける（モノレポ）。フロントエンドはバックエンドのHTTP APIのみを叩き、Firebaseへ直接アクセスしない。

```
/frontend            Next.js App Router（PWA対応、UIのみ）
  /app
    /onboarding      テーマ選択画面
    /home            番組準備完了画面
    /player          プレイヤー画面
  /public            PWAマニフェスト、アイコン
/backend             Go（バックエンドAPI・パイプライン実行）
  /cmd
    /server          main.go：HTTP APIサーバーのエントリポイント
    /demo            デモ用軽量パイプラインのCLIエントリポイント
  /internal
    /pipeline        収集→正規化→重複除去→重要度判定→台本化→音声化（オーケストレーション）
      fetch.go
      dedupe.go
      score.go
      script.go
      tts.go
    /providers       外部サービス実装（差し替え可能にする層）
      /fetcher
        types.go      共通インターフェース
        jina.go        jina.ai Reader実装（デフォルト）
        firecrawl.go   firecrawl実装（代替）
      /llm
        types.go      共通インターフェース
        gemini.go      Gemini実装（デフォルト）
      /tts
        types.go      共通インターフェース
        voicevox.go    VOICEVOX実装（デフォルト）
    /firebase        Firestore/Storageクライアント（Firebase Admin SDK for Go）
    /api             HTTPハンドラ（フロントエンドが呼ぶAPIエンドポイント、Cloud Schedulerからのバッチ起動を含む）
/docs
  /pipeline          パイプライン各ステップの詳細（実装ファイルと1対1対応）
  /features          画面・機能ごとの詳細（実装ディレクトリと1対1対応）
```

`internal/pipeline/*.go`は`internal/providers/*/types.go`のインターフェースだけを参照し、実装はプロバイダファイル単位で完結させる。プロバイダを変更する場合は環境変数（例：`TTS_PROVIDER=voicevox`）で切り替え、インターフェースを満たす新しいファイルを1つ追加するだけで済む設計にする（`pipeline`側のコードは変更不要）。
