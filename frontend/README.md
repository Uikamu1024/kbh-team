通学ラジオのフロントエンド（React + Vite製のPWA）。詳細な開発ガイドは[CLAUDE.md](CLAUDE.md)を参照。

## セットアップ

```bash
npm install
cp .env.example .env.local
```

## 開発サーバーの起動

```bash
npm run dev
```

[http://localhost:3000](http://localhost:3000) を開く。バックエンド（Go）を`http://localhost:8080`で起動しておくこと（[ルートのREADME](../README.md)参照）。

## その他のコマンド

```bash
npm run build    # 型チェック + 本番ビルド
npm run preview  # ビルド済み成果物のプレビュー
npm run lint     # ESLint
```
