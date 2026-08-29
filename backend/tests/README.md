# backend/tests

Goのユニットテスト（`*_test.go`）はGoの慣例通り、実装ファイルと同じパッケージ内に置く
（別ディレクトリに移すと`go test`が壊れる／非公開関数を直接テストできなくなるため）。
このディレクトリは**それとは別に**、手動での動作確認・結合確認用のスクリプトを置く場所。

## 中身

- `smoke.sh`：起動済みのバックエンドAPIサーバーに対して、ユーザー作成→タグ設定→
  デモ生成→番組取得→作り直しまでを一気通貫でcurlするスモークテスト。
  実装を大きく変更したときに手で動作確認する代わりに使う

## Goのユニットテストの場所（参考）

```
backend/internal/pipeline/dedupe_test.go
backend/internal/db/db_test.go       # DATABASE_URLが設定されている時だけ実行される統合テスト
backend/internal/storage/storage_test.go
```

実行方法：

```bash
cd backend
go test ./...                                   # DB接続が要らないテストのみ実行される
DATABASE_URL=postgres://... go test ./...       # internal/dbの統合テストも実行される
```

## smoke.shの使い方

```bash
cd backend
go run ./cmd/server &        # バックエンドを起動しておく（別ターミナルでも可）
./tests/smoke.sh             # デフォルトはhttp://localhost:8080
BASE_URL=http://localhost:9090 ./tests/smoke.sh   # ポートを変えたい場合
```

APIキー未設定（モックモード）でも通るように書かれている。VOICEVOXが起動していなくても
`demo/generate`はモック音声で成功する。
