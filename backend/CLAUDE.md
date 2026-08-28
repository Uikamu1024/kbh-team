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
    /api              HTTPハンドラ（フロントエンドが呼ぶAPIエンドポイント、バッチ起動用エンドポイントを含む。仕様は[docs/api-contract.yaml](../docs/api-contract.yaml)）
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

### `length_minutes`（番組の長さ設定）の反映先
プロフィール画面の「ポッドキャストの長さ」（`users.length_minutes`、5・10・15分のいずれか）は、
[④重要度判定](docs/pipeline/03-score.md)が記事を何件採用するかを決める入力になる：

- ③重複除去後の記事を重要度スコア順に並べ、各記事の推定尺（TTS前でも文字数からおおよそ算出できる。
  目安：日本語の読み上げは概ね300〜350字/分）を足し込みながら、累計が`length_minutes`を
  超えない範囲で上位から採用する
- 採用できる記事が無い・極端に少ない場合でも、最低1件は採用する（0チャプターの番組を作らない）
- `length_minutes`は`fetch.go`の取得件数（10〜20件固定）自体は変えない。あくまで③→④の
  「何件を番組に残すか」の閾値としてのみ使う（取得件数を絞ると重複除去・重要度判定の母数が
  減って精度が落ちるため）

### デモ用の軽量パイプライン
- 記事数を3件程度に絞った縮小版を別エントリポイント（`/backend/cmd/demo`）として用意
- その場でテーマを変更→数十秒〜1分で番組完成、を審査員の前でライブ実演する用途
- APIとしては`POST /api/demo/generate`（[docs/api-contract.yaml](../docs/api-contract.yaml)参照）で呼び出す

## バッチ実行
- ローカルでのスクリプト実行 or cronを想定（Cloud Schedulerは使わない。クラウドにデプロイしないため）
- APIとしては`POST /api/batch/run`（[docs/api-contract.yaml](../docs/api-contract.yaml)参照）から起動できるようにする
- 自動化方法の最終決定は[未決定事項](#未決定事項)を参照

### `delivery_time`とバッチの関係（重要）
プロフィール画面で配信時刻をユーザーごとに設定できるようになった（`users.delivery_time`）ため、
「毎朝6:00に全員分を一斉生成」という単純な発想のままだと、この設定が実装上ただの飾りになってしまう。
そこで`POST /api/batch/run`は**一斉生成のトリガーではなく、定期的にポーリングされる「今生成すべき人がいたら生成する」ハンドラ**として実装する：

1. cronは短い間隔（例：5〜10分おき）で`POST /api/batch/run`を叩く
2. ハンドラは全ユーザーを走査し、各ユーザーについて「現在時刻が`delivery_time`を過ぎていて、かつ今日分の`programs`行がまだ無い」場合にのみそのユーザーの番組を生成する
3. 生成した`programs.created_at`が「今日分」の判定に使われるので、二重生成は起きない

`delivery_time`のデフォルトは`06:00`なので、cronを6:00台に限定して動かしても既存の想定（毎朝6:00）と実質的に同じ挙動になる。ユーザーが時刻を変更した場合だけ、そのユーザーだけ別の時刻で生成される。

### 配信の「1日」の区切り
`delivery_time`は日付をまたぐ処理の基準にもなる。「今日」の定義は暦日（0:00）ではなく**その人の`delivery_time`**とする：

- `POST /api/users/{userId}/programs/latest/regenerate`の1日3回制限（`users.reset_count` / `users.reset_date`）も、リセット対象の「1日」はこの基準に揃える
- 判定ロジック（擬似コード）：
  ```
  today := 現在時刻が delivery_time より前なら「前日」、以降なら「当日」の日付
  if users.reset_date != today {
    users.reset_count = 0
    users.reset_date = today
  }
  if users.reset_count >= 3 { return 429 RESET_LIMIT_EXCEEDED }
  users.reset_count += 1
  ```
- 例：`delivery_time = 07:00`のユーザーが朝6:50にリセットを叩いた場合、「当日」はまだ前日扱いなので前日分のカウントを消費する

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
フロントエンドが呼ぶエンドポイントの仕様は[docs/api-contract.yaml](../docs/api-contract.yaml)に合意事項としてまとめている。**実装より先にこのファイルを更新して合意すること。**

`internal/api`配下に実装するハンドラの一覧（`internal/db`・`internal/pipeline`のどこに処理を委譲するかの目安）：

| メソッド・パス | 委譲先 |
| --- | --- |
| `POST /api/users` | `internal/db`（`users`行を1件作成するだけ） |
| `GET /api/users/{userId}` | `internal/db` |
| `PUT /api/users/{userId}/tags` | `internal/db` |
| `PUT /api/users/{userId}/settings` | `internal/db` |
| `GET /api/users/{userId}/programs/latest` | `internal/db` |
| `GET /api/users/{userId}/programs` | `internal/db` |
| `POST /api/users/{userId}/programs/latest/regenerate` | `internal/pipeline`一式を同期実行（[パイプライン概要](#パイプライン概要)参照）＋`internal/db`更新 |
| `GET /api/programs/{programId}` | `internal/db` |
| `GET /api/audio/{programId}/{chapterId}` | `internal/storage` |
| `POST /api/demo/generate` | `internal/pipeline`一式を同期実行（DB書き込みなし） |
| `POST /api/batch/run` | ユーザーごとに`internal/pipeline`一式を実行するかを判定（[`delivery_time`とバッチの関係](#delivery_timeとバッチの関係重要)参照）＋`internal/db`更新 |
| `GET /api/health` | `internal/db`・VOICEVOX ENGINEへの疎通確認 |

`GET /api/users/{userId}/programs/latest`のレスポンスに含める`Chapter.script`（読み上げ台本全文）は
[⑤要約・台本化](docs/pipeline/04-script.md)が`chapters.script`に書き込んだものをそのまま返す。
文単位のタイムスタンプは持たせない（プレイヤー側は経過秒数からの按分表示で妥協する設計。
[docs/api-contract.yaml](../docs/api-contract.yaml)の`x-open-questions`参照）。

## 実装上の注意点
- API制限を考慮：News API/RSS/LLM/TTSはいずれも呼び出し回数・レイテンシに制約がある。デモ本番用の音声は事前生成し、ライブデモ用には記事数を絞った軽量版パイプラインを別途用意する
- 記事取得元は最初からホワイトリスト化した数サイトに限定し、全サイト対応は行わない

## 未決定事項
- ホワイトリスト対象サイトの最終リスト
- Webフレームワーク（標準`net/http`のみで足りるか、chi等の軽量ルーターを使うか）
- `POST /api/batch/run`を叩くcronの間隔（5分？10分？[`delivery_time`とバッチの関係](#delivery_timeとバッチの関係重要)参照）と、ローカルでの自動化方法（cron／手動実行）
- `POST /api/users/{userId}/programs/latest/regenerate`の多重実行防止（同じuserIdから連打された場合の排他制御。`reset_count`の更新をトランザクションで囲むだけで足りるか、生成処理自体の二重起動も防ぐ必要があるか）
- `length_minutes`変更時に、次回配信からの反映でよいか、それとも当日分もリセット扱いにするか（現状は次回生成時から反映する想定）
