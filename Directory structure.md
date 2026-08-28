# ディレクトリ構成

開発が進むにつれて階層が変わる可能性が高いため、`CLAUDE.md`とは別ファイルで管理する。変更した場合はこのファイルを更新すること。

フロントエンド（Next.js）とバックエンド（Go）は別サービスとして分離し、同一リポジトリ内で`/frontend`と`/backend`に分ける（モノレポ）。フロントエンドはバックエンドのHTTP APIのみを叩く。クラウドへのデプロイは行わず、`docker-compose.yml`（ルート直下）でPostgreSQLとVOICEVOX ENGINEをローカル起動する。

各サービスの詳細なディレクトリ構成・実装方針は、それぞれの`CLAUDE.md`で管理する：
- フロントエンド：[frontend/CLAUDE.md](frontend/CLAUDE.md)
- バックエンド：[backend/CLAUDE.md](backend/CLAUDE.md)

## 全体像
```
/docker-compose.yml  PostgreSQL・VOICEVOX ENGINEをローカル起動する定義
/docs
  api-contract.md    フロントエンド⇔バックエンドのAPIコントラクト（共通）
/frontend            Next.js App Router（PWA対応、UIのみ）。詳細は frontend/CLAUDE.md
  /app
  /public
  /docs
    design.md
    /features
/backend             Go（バックエンドAPI・パイプライン実行）。詳細は backend/CLAUDE.md
  /cmd
  /internal
  /data
  /docs
    /pipeline
```

## この分割の考え方
- ルート直下：フロントエンド・バックエンド共通のドキュメント（要件、チーム開発の進め方、API契約、全体アーキテクチャ）
- `/frontend`配下：フロントエンド固有のドキュメント・実装（UI仕様、デザイン、画面ごとの詳細）
- `/backend`配下：バックエンド固有のドキュメント・実装（パイプライン各ステップ、プロバイダ実装、DBスキーマ）

`internal/pipeline/*.go`は`internal/providers/*/types.go`のインターフェースだけを参照し、実装はプロバイダファイル単位で完結させる。プロバイダを変更する場合は環境変数（例：`TTS_PROVIDER=voicevox`）で切り替え、インターフェースを満たす新しいファイルを1つ追加するだけで済む設計にする（`pipeline`側のコードは変更不要）。詳細は[backend/CLAUDE.md](backend/CLAUDE.md#プロバイダ層の設計原則)を参照。
