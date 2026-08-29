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

### 1. 共通インフラを起動（PostgreSQL・VOICEVOX）

リポジトリ直下で：

```bash
docker compose up -d
```

### 2. バックエンド（Go）

```bash
cd backend
cp .env.example .env
# .envに必要な値を埋める（PORT=8080、LLM_API_KEYなど）
go run ./cmd/server
```

`PORT`は[docs/api-contract.yaml](docs/api-contract.yaml)がポート8080前提になっているため、それに合わせること。

### 3. フロントエンド（React + Vite）

```bash
cd frontend
npm install
cp .env.example .env.local
npm run dev
```

[http://localhost:3000](http://localhost:3000) を開く。`frontend/vite.config.ts`で開発サーバーのポートを3000に固定しており、バックエンドのCORS許可オリジン（`http://localhost:3000`）と一致させている。

### 注意
フロントエンドは起動時に`POST /api/users`を呼ぶため、バックエンド（+ PostgreSQL）が先に起動している必要がある。バックエンドが起動していないと、フロントエンドは「バックエンドに接続できませんでした」という画面になる。

## チーム

| 役割 | 名前 | GitHub |
| --- | --- | --- |
|  |  | @ |

---

関西ビギナーズハッカソン vol.8 (2026/08/28-30)
