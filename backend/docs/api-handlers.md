# APIハンドラ実装ガイド
**対応ディレクトリ**: `/backend/internal/api`

[docs/api-contract.yaml](../../docs/api-contract.yaml)の各エンドポイントを、`internal/db`・`internal/pipeline`のどちらに処理を委譲して実装するかをまとめる。エンドポイントごとの詳細な仕様（リクエスト/レスポンス形式・エラーコード）はAPIコントラクト側が正、ここには**実装方針**だけを書く。

## エンドポイント一覧

| メソッド・パス | 委譲先 |
| --- | --- |
| `POST /api/users` | `internal/db`（`users`行を1件作成するだけ） |
| `GET /api/users/{userId}` | `internal/db` |
| `PUT /api/users/{userId}/tags` | `internal/db` |
| `PUT /api/users/{userId}/settings` | `internal/db` |
| `GET /api/users/{userId}/programs/latest` | `internal/db` |
| `GET /api/users/{userId}/programs` | `internal/db` |
| `POST /api/users/{userId}/programs/latest/regenerate` | [下記](#regenerateの多重リクエスト防止)参照。`internal/pipeline`一式を同期実行＋`internal/db`更新 |
| `GET /api/programs/{programId}` | `internal/db` |
| `GET /api/audio/{programId}/{chapterId}` | `internal/storage` |
| `POST /api/demo/generate` | `internal/pipeline`一式を同期実行（DB書き込みなし） |
| `POST /api/batch/run` | [下記](#delivery_timeとバッチの関係)参照。ユーザーごとに`internal/pipeline`一式を実行するかを判定＋`internal/db`更新 |
| `GET /api/health` | `internal/db`・VOICEVOX ENGINEへの疎通確認 |

`Chapter.script`（読み上げ台本全文）は[⑤要約・台本化](pipeline/04-script.md)が`chapters.script`に書き込んだものをそのまま返す。文単位のタイムスタンプは持たせない（プレイヤー側は経過秒数からの按分表示で妥協する設計。[docs/api-contract.yaml](../../docs/api-contract.yaml)の`x-open-questions`参照）。

## `delivery_time`とバッチの関係
プロフィール画面で配信時刻をユーザーごとに設定できる（`users.delivery_time`）ため、「毎朝6:00に全員一斉生成」という単純な実装だと、この設定が飾りになってしまう。`POST /api/batch/run`は**一斉生成のトリガーではなく、定期的にポーリングされる「今生成すべき人がいたら生成する」ハンドラ**として実装する：

1. cronは短い間隔（例：5〜10分おき）で`POST /api/batch/run`を叩く
2. ハンドラは全ユーザーを走査し、各ユーザーについて「現在時刻が`delivery_time`を過ぎていて、かつ今日分の`programs`行がまだ無い」場合にのみそのユーザーの番組を生成する
3. 生成した`programs.created_at`が「今日分」の判定に使われるので、二重生成は起きない

`delivery_time`のデフォルトは`06:00`なので、cronを6:00台に限定して動かしても既存の想定（毎朝6:00）と実質的に同じ挙動になる。ユーザーが時刻を変更した場合だけ、そのユーザーだけ別の時刻で生成される。

cronの実行間隔・自動化方法（cron／手動実行）自体は[backend/CLAUDE.md](../CLAUDE.md#未決定事項)の未決定事項を参照。

## 配信の「1日」の区切りとリセット回数
`delivery_time`は日付をまたぐ処理の基準にもなる。「今日」の定義は暦日（0:00）ではなく**その人の`delivery_time`**とする。`POST /api/users/{userId}/programs/latest/regenerate`の1日3回制限（`users.reset_count` / `users.reset_date`）もこの基準に揃える：

```
today := 現在時刻が delivery_time より前なら「前日」、以降なら「当日」の日付
if users.reset_date != today {
  users.reset_count = 0
  users.reset_date = today
}
if users.reset_count >= 3 { return 429 RESET_LIMIT_EXCEEDED }
users.reset_count += 1
```

例：`delivery_time = 07:00`のユーザーが朝6:50にリセットを叩いた場合、「当日」はまだ前日扱いなので前日分のカウントを消費する。

`length_minutes`を変更した場合は**次回生成時から**反映する（変更した当日にすでに生成済みの番組を作り直したり、リセット回数を消費させたりはしない）。

## `regenerate`の多重リクエスト防止
パイプライン全体の再実行は負荷が高いため、同じ`userId`に対する2つ目のリクエストは**パイプラインを起動する前に**弾く。1日3回という`reset_count`の上限とは別の防御線であることに注意（こちらは「同時に2つ動かさない」ための排他制御、`reset_count`は「1日に何回実行してよいか」の回数制限）。

- 実装はDBに状態を持たせず、**プロセス内メモリの排他制御**で十分（バックエンドはDockerで単一プロセス起動する前提のため。[backend/CLAUDE.md](../CLAUDE.md)参照）
- `userId`ごとに実行中フラグを持つマップ（例：`sync.Map`や`map[uuid.UUID]struct{}` + `sync.Mutex`）を用意し、ハンドラの先頭で次のように扱う：
  ```
  if 既にuserIdのフラグが立っている {
    return 409 ALREADY_GENERATING
  }
  フラグを立てる
  defer フラグを下ろす
  （パイプライン実行 → DB更新）
  ```
- バックエンドプロセスを再起動すればフラグは消える（永続化不要）。複数インスタンスでのスケールアウトは現状のアーキテクチャでは想定していない

## `POST /api/demo/generate`との違い
- `demo/generate`：`internal/pipeline`を実行するが**DB書き込みなし**。多重リクエスト防止も行わない（[docs/api-contract.yaml](../../docs/api-contract.yaml)の該当セクション参照）
- `regenerate`：`internal/pipeline`を実行し、結果で既存の`programs`/`chapters`行を**置き換える**。多重リクエスト防止・1日3回制限の両方がかかる
