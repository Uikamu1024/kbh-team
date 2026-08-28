# frontend/CLAUDE.md — フロントエンド開発ガイド

このファイルは`/frontend`配下で作業する際の前提知識。リポジトリ全体の方針は[ルートのCLAUDE.md](../CLAUDE.md)を参照。

## 概要
React + Vite製のPWA（SPA）。UIのみを担当し、データアクセスはすべてバックエンドAPI（Go）経由で行う。DB・音声ファイルへ直接アクセスしない。ルーティングはreact-router-dom（クライアントサイドのみ、SSRなし）。

## 技術スタック
- React + Vite（Tailwind CSS v4はPostCSSではなく`@tailwindcss/vite`プラグイン経由）、react-router-dom
- PWA対応：`public/manifest.webmanifest` + `public/icon.svg`（最小構成。オフラインキャッシュ用のService Workerは未導入）
- 実行：ローカル（`npm run dev` / `npm run build && npm run preview`）。デモも発表者のPC上でそのまま動かす想定
- 開発サーバーのポートは**3000固定**（`vite.config.ts`）。[docs/api-contract.yaml](../docs/api-contract.yaml)のCORS許可オリジン（`http://localhost:3000`）と一致させる必要があるため、変更する場合はバックエンド側のCORS設定も合わせて更新すること
- APIアクセス先：`VITE_API_BASE_URL`（バックエンドのURL、ローカルでは`http://localhost:8080`想定）

## 技術選定理由（既存決定）
- PWA化することで、ネイティブアプリストアの審査なしにホーム画面アイコンから起動でき、「1タップ起動」というMVP要件に直結する
- Next.js（App Router）から React + Vite への変更：SSR/サーバー機能は使わずバックエンドAPIを叩くだけのSPAであるため、Next.jsのサーバー機能は不要と判断。Viteは開発サーバーの起動・HMRが速く、2.5日のハッカソンでのイテレーション速度を優先した

クラウドへデプロイせずローカル完結とする全体方針の理由は[ルートのTech stack rationale.md](../Tech%20stack%20rationale.md)を参照。

## ディレクトリ構成
```
/frontend
  index.html           Viteのエントリーポイント
  vite.config.ts
  /public              PWAマニフェスト、アイコン
  /src
    main.tsx           エントリー（BrowserRouterのセットアップ）
    App.tsx             ルーティング定義
    /pages
      RootGate.tsx       起点。userId確認とオンボーディング要否の判定のみ行いリダイレクトする
      Onboarding.tsx     テーマ選択画面
      Home.tsx           番組準備完了画面
      Player.tsx         プレイヤー画面
      Profile.tsx        プロフィール画面（タグ編集・配信設定・番組リセット・統計）
    /components         画面間で共有するUI部品（AppShell / TopBar / BottomNav / TagPicker など）
    /lib                 型定義・APIクライアント・userId管理などの共通ロジック
  /docs
    /design.md       デザイン方針（Spotify参考）
    /features        画面ごとの仕様（実装ディレクトリと1対1対応）
```

`/app/profile`はルートの[Directory structure.md](../Directory%20structure.md)の概要図には無いが、[docs/api-contract.yaml](../docs/api-contract.yaml)のプロフィール関連エンドポイント（タグ・配信設定・番組リセット・統計）を利用する画面として追加した。

## 画面一覧
1. [オンボーディング](docs/features/onboarding.md)（テーマ選択、2〜3個まで）
2. [ホーム](docs/features/home.md)（今日の番組が準備完了、タップで再生）
3. [プレイヤー](docs/features/player.md)（チャプターリスト、記事リンク展開、シークバー）
4. プロフィール（タグ編集、配信時刻・番組の長さ設定、今日の番組の作り直し、統計。仕様は[docs/api-contract.yaml](../docs/api-contract.yaml)のUserProfile関連エンドポイント参照）

デザイン方針の詳細は[docs/design.md](docs/design.md)を参照。

## APIコントラクト
バックエンドAPIのエンドポイント・レスポンス形式は[docs/api-contract.yaml](../docs/api-contract.yaml)に合意事項としてまとめている。**実装より先にこのファイルを更新して合意すること。** 型定義は[src/lib/types.ts](src/lib/types.ts)に1対1で対応させている。

## 実装上の注意点
- **自動再生制限**：ブラウザはユーザー操作なしの音声自動再生をブロックする。ホーム画面の大きな再生ボタンをタップ→そのままプレイヤー画面へ遷移して再生開始、という導線で実現している（完全な自動再生は不可能な前提で設計。詳細は[docs/features/home.md](docs/features/home.md)参照）
- **userId**：ログイン機能はスコープ外のため、初回起動時に`POST /api/users`で発行された`userId`を`localStorage`に保存して使い回す（[src/lib/user.ts](src/lib/user.ts)）。表示名（ニックネーム）も同様にクライアントローカルのみで管理し、APIの対象外（[docs/api-contract.yaml](../docs/api-contract.yaml)参照）
- **プリセットタグ**：`src/lib/presetTags.ts`にハードコードしている（管理場所は[docs/api-contract.yaml](../docs/api-contract.yaml)のx-open-questions参照、未決定）
- **波形シークバー**：実データはAPIに存在しないため、`src/pages/Player.tsx`でチャプターIDから疑似生成した見た目用の波形を表示している（[docs/api-contract.yaml](../docs/api-contract.yaml)のx-open-questions参照）

## 環境変数
`.env.example`を各自コピーして`.env.local`を作成し、値を設定する（`.env*`はコミットしない）。Viteの仕様上、クライアントに公開する環境変数は`VITE_`プレフィックスが必須。

| 変数名 | 説明 |
| --- | --- |
| `VITE_API_BASE_URL` | バックエンドAPIのベースURL（ローカルでは`http://localhost:8080`想定） |
