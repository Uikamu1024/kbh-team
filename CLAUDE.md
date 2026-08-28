# CLAUDE.md — プロジェクト指示書（共通）

このファイルはClaude Codeがこのリポジトリで作業する際に最初に読み込む前提知識。フロントエンド・バックエンドどちらにも関わる共通事項のみをここに置き、各サービス固有の詳細はそれぞれのCLAUDE.mdに分割している。

- **フロントエンド固有**：[frontend/CLAUDE.md](frontend/CLAUDE.md)（Next.js、PWA、デザイン、画面仕様）
- **バックエンド固有**：[backend/CLAUDE.md](backend/CLAUDE.md)（Go、パイプライン、プロバイダ層、DB）

その他の共通ドキュメント：
- 機能要件：[Requirements.md](Requirements.md)
- ディレクトリ構成：[Directory structure.md](Directory%20structure.md)
- 技術選定理由（全体方針）：[Tech stack rationale.md](Tech%20stack%20rationale.md)
- チーム開発の進め方（ブランチ運用・役割分担）：[Team workflow.md](Team%20workflow.md)
- フロントエンド⇔バックエンドのAPIコントラクト：[docs/api-contract.md](docs/api-contract.md)
- 環境変数：[frontend/.env.example](frontend/.env.example) / [backend/.env.example](backend/.env.example)

## プロジェクト概要
通学中に聞ける、テーマ登録型のパーソナルAIラジオPWA。関西ビギナーズハッカソン vol.8（2.5日開発）向けのプロトタイプ。詳細は[Requirements.md](Requirements.md)を参照。

## 全体アーキテクチャ
チーム開発かつハッカソンのため、**無償かつできるだけローカルで完結する構成**を採用する。クラウドへのデプロイは行わず、`docker compose up`でチーム全員が同じ環境を再現できることを優先する。フロントエンドとバックエンドは別サービスとして分離する（学習目的でバックエンドにGoを使う）。

| 領域 | 技術 | 詳細 |
| --- | --- | --- |
| フロントエンド | Next.js（App Router）、PWA対応 | [frontend/CLAUDE.md](frontend/CLAUDE.md) |
| バックエンド | Go（標準`net/http`） | [backend/CLAUDE.md](backend/CLAUDE.md) |
| データベース | PostgreSQL（Dockerでローカル起動） | [backend/CLAUDE.md](backend/CLAUDE.md) |
| ファイルストレージ | ローカルファイルシステム | [backend/CLAUDE.md](backend/CLAUDE.md) |
| 記事取得 | jina.ai Reader／firecrawl free tier | [backend/docs/pipeline/01-fetch.md](backend/docs/pipeline/01-fetch.md) |
| LLM | Gemini API（無料枠） | [backend/docs/pipeline/04-script.md](backend/docs/pipeline/04-script.md) |
| TTS | VOICEVOX（Dockerでローカル起動） | [backend/docs/pipeline/05-tts.md](backend/docs/pipeline/05-tts.md) |
| バッチ実行 | ローカルスクリプト実行 or cron | [backend/CLAUDE.md](backend/CLAUDE.md#バッチ実行) |

各技術の選定理由は、全体方針に関わるものは[Tech stack rationale.md](Tech%20stack%20rationale.md)、個別のものは各サービスのCLAUDE.mdを参照。

## 開発方針・優先順位
1. **保守性を優先**：記事取得・LLM・TTSは`/backend/internal/providers`配下にプロバイダ単位でファイル分割し、共通インターフェース（`types.go`）経由で`/backend/internal/pipeline`から呼び出す。TTSやLLMのプロバイダ（VOICEVOXやGeminiなど）を途中で変える可能性があるため、実装差し替え時に他のコードへ影響が及ばないようにする（詳細：[backend/CLAUDE.md](backend/CLAUDE.md#プロバイダ層の設計原則)）
2. **フロントエンド／バックエンドは疎結合に**：フロントエンドはバックエンドのHTTP APIのみを叩く。[docs/api-contract.md](docs/api-contract.md)でAPIのレスポンス形式（JSON）を先に決めてから両方の実装に着手する（進め方の詳細は[Team workflow.md](Team%20workflow.md)参照）
3. フェーズ分けで進める：
   - Phase 1：Goで1テーマ収集→要約→TTSの一気通貫パイプラインを通す（モックデータでもいい、CLIでも可）
   - Phase 2：PostgreSQL/ローカルストレージ連携、バックエンドAPI化、複数テーマ対応
   - Phase 3：フロントエンドからAPI経由でプレイヤーUI・1タップ起動を実装
   - Phase 4：重要度判定・会話形式TTS・バッチ実行の自動化などの磨き込み
4. [Requirements.md](Requirements.md)の「スコープ外」に書かれた機能は、明示的な指示がない限り実装しない

## 現時点の未決定事項（着手前に確認）
プロダクト全体に関わるものをここに置く。各サービス固有の未決定事項は該当するCLAUDE.mdに記載している（[backend/CLAUDE.md](backend/CLAUDE.md#未決定事項)）。

- `docs/api-contract.md`のエラーレスポンス形式、`userId`の識別方法（ログイン機能はスコープ外のため暫定対応が必要。フロントエンド・バックエンド双方の合意が必要）
- 審査員に共有する「デモURL」をどうするか（README.mdに項目があるが、完全ローカル構成だと公開URLがない。発表者のPCでライブ実演のみにするか、デモ時だけ一時的に公開するか）
