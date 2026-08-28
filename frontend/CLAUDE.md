@AGENTS.md

# frontend/CLAUDE.md — フロントエンド開発ガイド

このファイルは`/frontend`配下で作業する際の前提知識。リポジトリ全体の方針は[ルートのCLAUDE.md](../CLAUDE.md)を参照。上の`@AGENTS.md`行は`next dev`が自動生成・再追加するため触らないこと。

## 概要
Next.js（App Router）製のPWA。UIのみを担当し、データアクセスはすべてバックエンドAPI（Go）経由で行う。DB・音声ファイルへ直接アクセスしない。

## 技術スタック
- Next.js（App Router）、PWA対応
- 実行：ローカル（`npm run dev` / `next start`）。デモも発表者のPC上でそのまま動かす想定
- APIアクセス先：`NEXT_PUBLIC_API_BASE_URL`（バックエンドのURL、ローカルでは`http://localhost:8080`想定）

## 技術選定理由（既存決定）
- PWA化することで、ネイティブアプリストアの審査なしにホーム画面アイコンから起動でき、「1タップ起動」というMVP要件に直結する
- Next.jsは開発のデファクトスタンダードで、Vercelとの相性が良くハッカソンでの立ち上げが速い

クラウドへデプロイせずローカル完結とする全体方針の理由は[ルートのTech stack rationale.md](../Tech%20stack%20rationale.md)を参照。

## ディレクトリ構成
```
/frontend
  /app
    /onboarding      テーマ選択画面
    /home            番組準備完了画面
    /player          プレイヤー画面
  /public            PWAマニフェスト、アイコン
  /docs
    /design.md       デザイン方針（Spotify参考）
    /features        画面ごとの仕様（実装ディレクトリと1対1対応）
```

## 画面一覧
1. [オンボーディング](docs/features/onboarding.md)（テーマ選択、2〜3個まで）
2. [ホーム](docs/features/home.md)（今日の番組が準備完了、タップで再生）
3. [プレイヤー](docs/features/player.md)（チャプターリスト、記事リンク展開、シークバー）

デザイン方針の詳細は[docs/design.md](docs/design.md)を参照。

## APIコントラクト
バックエンドAPIのエンドポイント・レスポンス形式は[docs/api-contract.md](../docs/api-contract.md)に合意事項としてまとめている。**実装より先にこのファイルを更新して合意すること。** 並行開発中は、このJSON形状に沿ったモックデータでUIを作り、バックエンドの完成を待たない（詳細は[Team workflow.md](../Team%20workflow.md)参照）。

## 実装上の注意点
- **自動再生制限**：ブラウザはユーザー操作なしの音声自動再生をブロックする。ホーム画面アイコンのタップ→即座に再生開始、という1タップ導線で実現すること（完全な自動再生は不可能な前提で設計する。詳細は[docs/features/home.md](docs/features/home.md)参照）

## 環境変数
`.env.example`を各自コピーして`.env.local`を作成し、値を設定する（`.env*`はコミットしない）。

| 変数名 | 説明 |
| --- | --- |
| `NEXT_PUBLIC_API_BASE_URL` | バックエンドAPIのベースURL（ローカルでは`http://localhost:8080`想定） |
