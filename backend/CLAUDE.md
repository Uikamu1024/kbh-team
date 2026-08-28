# backend/CLAUDE.md — バックエンド開発ガイド

このファイルは`/backend`配下で作業する際の前提知識。リポジトリ全体の方針は[ルートのCLAUDE.md](../CLAUDE.md)を参照。

## 概要
Go製のAPIサーバー兼パイプライン実行基盤。フロントエンド（Next.js）はこのバックエンドのHTTP APIのみを叩き、DB・外部API・音声ファイルへは直接アクセスしない。

## 技術スタック
- **言語／フレームワーク**：Go、標準`net/http`（ルーターは軽量ライブラリの採用可否も含め未決定。[未決定事項](#未決定事項)参照）
- **データベース**：PostgreSQL（`docker-compose.yml`でローカル起動）
- **ファイルストレージ**：ローカルファイルシステム（`/backend/data/audio`、gitignore対象）
- **記事取得**：jina.ai Reader（第一候補）／firecrawl free tier（代替）※要ネットワーク接続
- **LLM**：Gemini API（Google AI Studio 無料枠）※要ネットワーク接続
- **TTS**：VOICEVOX（Dockerでローカル起動）
- **バッチ実行**：ローカルスクリプト実行 or cron（詳細は[未決定事項](#未決定事項)参照）

実行方法：ローカルバイナリ（`go run ./cmd/server`）／Docker。

## ディレクトリ構成
```
/backend
  /cmd
    /server          main.go：HTTP APIサーバーのエントリポイント
    /demo             デモ用軽量パイプラインのCLIエントリポイント
  /internal
    /pipeline         収集→正規化→重複除去→重要度判定→台本化→音声化（オーケストレーション）
      fetch.go
      dedupe.go
      score.go
      script.go
      tts.go
    /providers        外部サービス実装（差し替え可能にする層）
      /fetcher
        types.go       共通インターフェース
        jina.go         jina.ai Reader実装（デフォルト）
        firecrawl.go    firecrawl実装（代替）
      /llm
        types.go       共通インターフェース
        gemini.go       Gemini実装（デフォルト）
      /tts
        types.go       共通インターフェース
        voicevox.go     VOICEVOX実装（デフォルト、ローカルのVOICEVOX ENGINEを呼ぶ）
    /db               PostgreSQLクライアント（データモデルは[06-storage.md](docs/pipeline/06-storage.md)参照）
    /storage          音声ファイルの読み書き（ローカルファイルシステム、`/backend/data/audio`配下）
    /api              HTTPハンドラ（フロントエンドが呼ぶAPIエンドポイント、バッチ起動用エンドポイントを含む。仕様は[docs/api-contract.md](../docs/api-contract.md)）
  /data
    /audio            生成した音声ファイルの保存先（gitignore対象）
  /docs
    /pipeline         パイプライン各ステップの詳細（実装ファイルと1対1対応）
```

## プロバイダ層の設計原則
`internal/pipeline/*.go`は`internal/providers/*/types.go`のインターフェースだけを参照し、実装はプロバイダファイル単位で完結させる。LLMやTTSのプロバイダ（VOICEVOXやGeminiなど）を途中で変える可能性があるため、こう設計している：

- プロバイダを変更する場合は環境変数（例：`TTS_PROVIDER=voicevox`）で切り替え、インターフェースを満たす新しいファイルを1つ追加するだけで済む
- `pipeline`側のコードは変更不要
- 例：VOICEVOXが使えなくなった場合でも、`providers/tts/types.go`を満たす新規実装（例：Google Cloud TTS）を1ファイル追加するだけでよい

## パイプライン概要
```
①収集 → ②正規化 → ③重複除去 → ④重要度判定 → ⑤要約・台本化 → ⑥音声化 → ⑦保存
```
各ステップの詳細は実装ファイル単位（`/backend/internal/pipeline/*.go`）で分割し、`docs/pipeline/`配下に置く。

- [①② 収集・正規化](docs/pipeline/01-fetch.md) — `fetch.go`
- [③ 重複除去](docs/pipeline/02-dedupe.md) — `dedupe.go`
- [④ 重要度判定](docs/pipeline/03-score.md) — `score.go`
- [⑤ 要約・台本化](docs/pipeline/04-script.md) — `script.go`
- [⑥ 音声化](docs/pipeline/05-tts.md) — `tts.go`
- [⑦ 保存](docs/pipeline/06-storage.md) — `internal/db`, `internal/storage`

### デモ用の軽量パイプライン
- 記事数を3件程度に絞った縮小版を別エントリポイント（`/backend/cmd/demo`）として用意
- その場でテーマを変更→数十秒〜1分で番組完成、を審査員の前でライブ実演する用途
- APIとしては[`POST /api/demo/generate`](../docs/api-contract.md#post-apidemogenerate)で呼び出す

## バッチ実行
- 生成バッチは固定時刻（毎朝6:00想定）で実行する設計とする
- ローカルでのスクリプト実行 or cronを想定（Cloud Schedulerは使わない。クラウドにデプロイしないため）
- APIとしては[`POST /api/batch/run`](../docs/api-contract.md#post-apibatchrun)から起動できるようにし、全ユーザー分の番組を生成する
- 自動化方法の最終決定は[未決定事項](#未決定事項)を参照

## 技術選定理由
`（既存決定）`と付いている項目は、ドキュメント整理より前にプロジェクトの前提として決まっていたもの。

- **Go（標準net/http）**：チームメンバーがバックエンドでGoを試してみたいという学習目的の希望から採用。フロントエンドと責務を明確に分離するため、別サービスとして切り出した
- **記事取得：jina.ai Reader／firecrawl**（既存決定）：jina.ai Readerは無料枠があり、URLを渡すだけで本文をMarkdown化して取得できる手軽さがある。詰まった場合の代替としてfirecrawlのfree tierを用意し、単一障害点にならないようにしている
- **LLM：Gemini API**：Claude APIには恒常的な無料枠がなく、トライアルクレジットのみのため「無償」の制約に合わない。Geminiは無料枠が継続的に使え、記事の要約・台本生成の用途には十分な性能
- **TTS：VOICEVOX**：完全無料・オープンソースで、TTS APIの中でも珍しく無料枠の心配が要らない。キャラクターごとに声が異なるため、単調さ対策として決めた「2人の話者による会話形式」というMVP要件にそのまま合致する。日本語ハッカソンでの採用実績が多く、ドキュメントも豊富
- **データベース：PostgreSQL**：当初はFirebase（Firestore + Storage）を予定していたが、チームメンバーが「Firebaseを使いたくない」「できるだけローカルで完結したい」と希望したため変更した。Firebaseのような外部クラウドサービスへの依存をなくし、`docker compose up`でDBを含め全員が同じ環境を再現できるようにするため
- **ファイルストレージ：ローカルファイルシステム**：PostgreSQL化にあわせてFirebase Storageも廃止。生成した音声ファイルはバックエンドのローカルディスク（`/backend/data/audio`）に保存し、バックエンドAPIが配信する。外部ストレージサービスへの依存・課金設定が不要になる
- **プロバイダ層の分離設計**：[上記](#プロバイダ層の設計原則)参照

ローカル完結・クラウド非デプロイという全体方針の理由は[ルートのTech stack rationale.md](../Tech%20stack%20rationale.md)を参照。

## 環境変数
`.env.example`を各自コピーして`.env`を作成し、値を設定する（`.env`はコミットしない）。

| 変数名 | 説明 |
| --- | --- |
| `PORT` | APIサーバーの待受ポート |
| `DATABASE_URL` | PostgreSQL接続文字列 |
| `AUDIO_STORAGE_PATH` | 生成した音声ファイルの保存先パス |
| `LLM_API_KEY` | Gemini APIキー |
| `TTS_API_KEY` | TTSプロバイダのAPIキー（VOICEVOXはローカル起動のため通常不要） |
| `JINA_AI_API_KEY` | jina.ai Reader APIキー |
| `FIRECRAWL_API_KEY` | firecrawl APIキー（代替プロバイダ用） |
| `VOICEVOX_ENGINE_URL` | ローカルVOICEVOX ENGINEのURL（`docker-compose.yml`参照） |

## APIコントラクト
フロントエンドが呼ぶエンドポイントの仕様は[docs/api-contract.md](../docs/api-contract.md)に合意事項としてまとめている。**実装より先にこのファイルを更新して合意すること。**

## 実装上の注意点
- API制限を考慮：News API/RSS/LLM/TTSはいずれも呼び出し回数・レイテンシに制約がある。デモ本番用の音声は事前生成し、ライブデモ用には記事数を絞った軽量版パイプラインを別途用意する
- 記事取得元は最初からホワイトリスト化した数サイトに限定し、全サイト対応は行わない

## 未決定事項
- ホワイトリスト対象サイトの最終リスト
- Webフレームワーク（標準`net/http`のみで足りるか、chi等の軽量ルーターを使うか）
- `docs/api-contract.md`のエラーレスポンス形式、`userId`の識別方法（ログイン機能はスコープ外のため暫定対応が必要）
- 毎朝6:00のバッチ実行をローカルでどう自動化するか（cron／手動実行でよいか）
