# プロダクト名

> 一言でどんなプロダクトか（30字以内）

## デモ

<!-- スクリーンショット or GIF をここに貼る。動くものが一番強い -->

- デモURL：
- 動画：

## 課題

誰の、どんな困りごとを解決しますか？

## 解決方法

どうやって解決しますか？特徴を3つまで。

-
-
-

## 使用技術

| 領域 | 技術 |
| --- | --- |
| フロントエンド |  |
| バックエンド |  |
| その他 |  |

## 動かし方

初めての人向けに、必要なツールのインストールから順番に説明する。すでに入っているものは読み飛ばしてOK。

### 0. 事前準備（インストールするもの）

| ツール | 用途 | 確認コマンド |
| --- | --- | --- |
| [Git](https://git-scm.com/) | リポジトリの取得 | `git --version` |
| [Docker Desktop](https://www.docker.com/products/docker-desktop/) | PostgreSQL・VOICEVOXをローカルで動かす | `docker --version` |
| [Node.js](https://nodejs.org/)（20以上推奨） | フロントエンド（npm） | `node --version` |
| [Go](https://go.dev/dl/)（1.27以上） | バックエンド | `go version` |

ターミナルで確認コマンドを打ってバージョンが表示されればインストール済み。`command not found`のようなエラーが出たら、リンク先から公式インストーラーをダウンロードして入れる。

**Docker Desktopは「インストールしただけ」では動かない。** インストール後、PCのアプリ一覧（macOSならLaunchpad、WindowsならスタートメニューWindows）から「Docker Desktop」を探して起動し、アプリ内のクジラのアイコンが動いて「Docker Desktop is running」のような表示になるまで待つ。これを忘れて次の手順に進むと`docker compose up`が失敗する。

### 1. リポジトリを取得

```bash
git clone <このリポジトリのURL>
cd kbh-team
```

（すでに手元にある場合はこの手順は不要）

### 2. 共通インフラを起動（PostgreSQL・VOICEVOX）

Docker Desktopが起動している状態で、リポジトリ直下（`kbh-team/`）で実行：

```bash
docker compose up -d
```

初回は使用するイメージのダウンロードが走るため、数分かかることがある（回線速度による）。`-d`はバックグラウンド実行の意味で、コマンドがすぐ返ってくる。

起動できたか確認：

```bash
docker compose ps
```

`postgres`と`voicevox`の2つが`running`（または`Up`）になっていればOK。

### 3. バックエンド（Go）

新しいターミナルを開いて：

```bash
cd backend
cp .env.example .env
```

作成した`backend/.env`をエディタで開き、値を埋める（`PORT=8080`、LLM/TTSなど各APIキー。取得方法はチーム内で共有されているものを使う）。`PORT`は[docs/api-contract.yaml](docs/api-contract.yaml)がポート8080前提になっているため、変更しないこと。

```bash
go run ./cmd/server
```

エラーなく起動し、ターミナルが待機状態のままになれば成功（このターミナルは開いたままにしておく）。

### 4. フロントエンド（React + Vite）

さらに新しいターミナルを開いて：

```bash
cd frontend
npm install
cp .env.example .env.local
npm run dev
```

ターミナルに表示される [http://localhost:3000](http://localhost:3000) をブラウザで開く。`frontend/vite.config.ts`で開発サーバーのポートを3000に固定しており、バックエンドのCORS許可オリジン（`http://localhost:3000`）と一致させているため、ポート番号は変更しないこと。

### 全体の起動順序まとめ

1. Docker Desktopを起動する（アプリを開いて待つ）
2. `docker compose up -d`（PostgreSQL・VOICEVOX）
3. `go run ./cmd/server`（バックエンド）← ターミナル1つ使う、開いたままにする
4. `npm run dev`（フロントエンド）← 別のターミナルを使う、開いたままにする
5. ブラウザで`http://localhost:3000`を開く

フロントエンドは起動時に`POST /api/users`を呼ぶため、**バックエンド（+ PostgreSQL）が先に起動している必要がある**。バックエンドが起動していないと、フロントエンドは「バックエンドに接続できませんでした」という画面になる（その場合は上から順に確認し直す）。

### うまく動かないとき

- **`docker compose up`が失敗する** → Docker Desktopアプリが起動しているか確認する
- **フロントエンドが「バックエンドに接続できませんでした」と表示される** → バックエンド（`go run ./cmd/server`）が起動しているか、ターミナルにエラーが出ていないか確認する
- **ポートが使用中というエラーが出る（`address already in use`など）** → 8080番（バックエンド）や3000番・5432番・50021番（Docker）を他のアプリがすでに使っている可能性がある。心当たりのあるアプリ・古いターミナルを閉じてから再度試す
- **`.env`を編集したのに反映されない** → バックエンド（`go run`）・フロントエンド（`npm run dev`）を一度Ctrl+Cで止めて起動し直す

## チーム

| 役割 | 名前 | GitHub |
| --- | --- | --- |
|  |  | @ |

---

関西ビギナーズハッカソン vol.8 (2026/08/28-30)
