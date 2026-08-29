# backend

Go製のAPIサーバー兼パイプライン実行基盤。全体のセットアップ手順（Docker Desktop・
PostgreSQL・VOICEVOXの起動を含む）は[ルートのREADME](../README.md)を参照。ここでは
backend単体を動かす・テストする際のコマンドをまとめる。開発方針・設計は
[CLAUDE.md](CLAUDE.md)、詳細仕様は[docs/](docs/)配下を参照。

## セットアップ

```bash
cd backend
cp .env.example .env
```

`.env`を編集してAPIキー等を設定する（未設定の項目はモックモードで動く。[.env.example](.env.example)参照）。

## 起動方法

### APIサーバー（Phase 2以降・PostgreSQL接続あり）

```bash
docker compose up -d          # リポジトリ直下でPostgreSQL・VOICEVOXを起動
go run ./cmd/server           # backend配下で実行。http://localhost:8080 で待受
```

### デモ用の軽量パイプラインCLI（Phase 1・DB接続不要）

```bash
go run ./cmd/demo AI 京都
```

タグを引数で渡すと、記事取得→重複除去→重要度判定→台本生成→音声合成までを1回実行し、
結果を標準出力に、音声を`./data/audio/demo/`に保存する。APIキー未設定でもモックモードで
最後まで通る。

## テスト

```bash
go test ./...                              # 単体テスト（DB接続不要な範囲）
DATABASE_URL=postgres://... go test ./...  # internal/dbの統合テストも実行
gofmt -l .                                 # フォーマットチェック
go vet ./...
```

`go run ./cmd/server`を起動した状態で、エンドツーエンドの簡易確認をしたい場合：

```bash
./tests/smoke.sh
```

詳細は[tests/README.md](tests/README.md)を参照（単体テストと手動確認用スクリプトの
置き場所を分けている理由もここに書いてある）。

## ディレクトリ構成

詳細は[CLAUDE.md](CLAUDE.md#ディレクトリ構成)を参照。テスト関連だけ補足すると：

```
/backend
  /internal/**/*_test.go   Goの単体テスト（実装ファイルと同じパッケージに配置。Go標準の慣例）
  /tests                    手動での動作確認用スクリプト（smoke.sh等。Goのテストではない）
```
