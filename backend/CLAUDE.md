# backend/CLAUDE.md — バックエンド開発ガイド

このファイルは`/backend`配下で作業する際の前提知識。**詳細な仕様はここに書かず`docs/`配下に置く**（このファイルは作業開始時に毎回読み込まれるため、簡潔に保つ）。リポジトリ全体の方針は[ルートのCLAUDE.md](../CLAUDE.md)を参照。

## 概要
Go製のAPIサーバー兼パイプライン実行基盤。フロントエンド（Next.js）はこのバックエンドのHTTP APIのみを叩き、DB・外部API・音声ファイルへは直接アクセスしない。

## 技術スタック
- **言語／フレームワーク**：Go、標準`net/http`（Go 1.22以降の`http.ServeMux`はメソッド・パスパラメータ付きパターン（例：`"POST /api/users/{userId}/tags"`）に対応しているため、軽量ルーターの追加導入は不要と判断・決定した）
- **データベース**：PostgreSQL（`docker-compose.yml`でローカル起動）
- **ファイルストレージ**：ローカルファイルシステム（`/backend/data/audio`、gitignore対象）
- **記事取得**：jina.ai Reader（第一候補）／firecrawl free tier（代替）
- **LLM**：Gemini API（Google AI Studio 無料枠）
- **TTS**：VOICEVOX（Dockerでローカル起動）

実行方法：ローカルバイナリ（`go run ./cmd/server`）／Docker。選定理由は[docs/tech-stack.md](docs/tech-stack.md)を参照。

## ディレクトリ構成
```
/backend
  /cmd
    /server          main.go：HTTP APIサーバーのエントリポイント
    /demo             デモ用軽量パイプラインのCLIエントリポイント
    /gentrace         記事音声生成のトレース・デバッグ用CLI（外部HTTP通信を記録しながら手動検証する）
  /internal
    /domain           パイプライン全体で共有するデータ型（Article, Topic, ScoredTopic等）
    /pipeline         収集→正規化→重複除去→重要度判定→台本化→音声化（オーケストレーション）
    /providers        外部サービス実装（差し替え可能にする層。/fetcher, /llm, /tts）
    /db               PostgreSQLクライアント
    /storage          音声ファイルの読み書き（ローカルファイルシステム）
    /api              HTTPハンドラ
    /envfile          .env読み込み（Goに無い機能を補う自前ローダー）
    /trace            gentrace用のHTTPリクエスト/レスポンス記録（本番コードからは使われない）
  /data/audio         生成した音声ファイルの保存先（gitignore対象）
  /docs
    api-handlers.md   APIハンドラの実装方針（エンドポイント一覧・バッチ/リセットのロジック）
    tech-stack.md     技術選定理由
    /pipeline         パイプライン各ステップの詳細仕様（実装ファイルと1対1対応）
    /generation       記事収集の再設計（RSSキャッシュ方式への移行、査読待ち。下記「未決定事項」参照）
```

## プロバイダ層の設計原則
`internal/pipeline/*.go`は`internal/providers/*/types.go`のインターフェースだけを参照し、実装はプロバイダファイル単位で完結させる。プロバイダ変更時は環境変数（例：`TTS_PROVIDER=voicevox`）で切り替え、インターフェースを満たす新しいファイルを1つ追加するだけで済む（`pipeline`側は変更不要）。

## パイプライン概要
```
①収集 → ②正規化 → ③重複除去 → ④重要度判定 → ⑤要約・台本化 → ⑥音声化 → ⑦保存
```
各ステップの入力・出力・実装方針は`docs/pipeline/`配下に1ファイルずつある。**実装前に必ず読むこと**（データの型・フィールド名まで規定している）：

- [①② 収集・正規化](docs/pipeline/01-fetch.md) — `fetch.go`
- [③ 重複除去](docs/pipeline/02-dedupe.md) — `dedupe.go`
- [④ 重要度判定](docs/pipeline/03-score.md) — `score.go`（番組の長さ設定`length_minutes`の反映もここ）
- [⑤ 要約・台本化](docs/pipeline/04-script.md) — `script.go`
- [⑥ 音声化](docs/pipeline/05-tts.md) — `tts.go`
- [⑦ 保存](docs/pipeline/06-storage.md) — `internal/db`, `internal/storage`（PostgreSQLスキーマもここ）

デモ用の軽量パイプライン（記事数を絞った縮小版、`POST /api/demo/generate`）とバッチ実行（`POST /api/batch/run`）の実装方針は[docs/api-handlers.md](docs/api-handlers.md)を参照。

## 開発フェーズとの対応
[ルートCLAUDE.md](../CLAUDE.md#開発方針優先順位)のPhase 1〜5に対応するバックエンド側のスコープ：

| Phase | バックエンドのスコープ |
| --- | --- |
| 1 | `internal/pipeline`一気通貫（CLI可）。`docs/pipeline/01〜06`の①〜⑥ |
| 2 | PostgreSQL/ローカルストレージ連携（⑦保存）、`internal/api`でのAPI化 |
| 3 | フロントエンドから叩かれる基本エンドポイント（users/tags/programs/audio）を安定させる |
| 4 | 重要度判定の精度、バッチ自動化 |
| 5 | プロフィール周り（`settings`・履歴・`regenerate`）。[docs/api-handlers.md](docs/api-handlers.md)参照 |

Phase 1〜3が通るまでPhase 5の機能（設定・履歴・作り直し）に着手しない。

## 環境変数
`.env.example`を各自コピーして`.env`を作成し、値を設定する（`.env`はコミットしない）。

| 変数名 | 説明 |
| --- | --- |
| `PORT` | APIサーバーの待受ポート |
| `DATABASE_URL` | PostgreSQL接続文字列 |
| `AUDIO_STORAGE_PATH` | 生成した音声ファイルの保存先パス |
| `LLM_API_KEY` | Gemini APIキー |
| `LLM_PROVIDER` | 使用するLLMプロバイダ（`gemini`（デフォルト）／`openrouter`／`ollama`。`internal/providers/llm/from_env.go`参照） |
| `OPENROUTER_API_KEY` / `OPENROUTER_MODEL` | OpenRouter利用時のAPIキー・モデル名 |
| `OLLAMA_API_KEY` / `OLLAMA_MODEL` | Ollama Cloud利用時のAPIキー・モデル名 |
| `TTS_API_KEY` | TTSプロバイダのAPIキー（VOICEVOXはローカル起動のため通常不要） |
| `JINA_AI_API_KEY` | jina.ai Reader APIキー |
| `FIRECRAWL_API_KEY` | firecrawl APIキー（代替プロバイダ用） |
| `VOICEVOX_ENGINE_URL` | ローカルVOICEVOX ENGINEのURL（`docker-compose.yml`参照） |
| `DEMO_MODE` | 設定すると`POST /api/batch/run`・`POST /api/demo/generate`を無効化し、`POST /api/users/{userId}/programs/latest/regenerate`と`GET .../programs/latest`・`GET .../programs`は実生成の代わりに既存の`programs`行をユーザーへランダムに割り当てる（`demo_program_assignments`テーブル、`internal/db/demo.go`）。重い生成処理を避けたいデモ実演用 |

## APIコントラクト
フロントエンドが呼ぶエンドポイントの仕様は[docs/api-contract.yaml](../docs/api-contract.yaml)に合意事項としてまとめている。**実装より先にこのファイルを更新して合意すること。** 各エンドポイントの実装方針（委譲先・バッチ/リセットのロジック）は[docs/api-handlers.md](docs/api-handlers.md)を参照。

## 実装上の注意点
- API制限を考慮：記事取得・LLM・TTSはいずれも呼び出し回数・レイテンシに制約がある。デモ本番用の音声は事前生成し、ライブデモ用には記事数を絞った軽量版パイプライン（`POST /api/demo/generate`）を使う
- 記事取得元は最初からホワイトリスト化した数サイトに限定し、全サイト対応は行わない

## 未決定事項
- **記事収集の再設計（査読待ち）**：現行の`internal/providers/fetcher/jina.go`はタグ名→ダミーURLの固定辞書（`testWhitelist`）に依存する仮実装で、実サイトの記事を動的に収集する仕組みが無い。これを「RSSフィード一覧→収集ジョブ（`cmd/ingest`、新規）が定期的にキャッシュ→生成時はキャッシュSELECTのみ」という方式に置き換える設計を[docs/generation/](docs/generation/)にまとめてあり、現在ユーザーによる査読中。承認され次第、本セクション・[パイプライン概要](#パイプライン概要)・[docs/pipeline/01-fetch.md](docs/pipeline/01-fetch.md)・[02-dedupe.md](docs/pipeline/02-dedupe.md)をこの決定事項として更新する。**着手前に必ず[docs/generation/README.md](docs/generation/README.md)を読むこと**（ホワイトリスト対象サイトの最終リスト自体は、この設計変更後も別途未決定のまま）

## 決定事項（旧・未決定事項）
- **Webフレームワーク**：標準`net/http`の`http.ServeMux`（Go 1.22+のパスパターン機能）のみで実装する。軽量ルーターは導入しない
- **`POST /api/batch/run`を叩くcronの実行間隔とローカルでの自動化方法**：5分間隔。OS標準のcron（`crontab`）から`curl`で叩く方式とし、`backend/scripts/batch-cron.sh`と設定例を用意する（実際に開発者のcrontabへ登録するかは各自の判断）
