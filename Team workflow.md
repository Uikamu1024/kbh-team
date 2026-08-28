# チーム開発の進め方

2.5日のハッカソンでの並行開発の進め方をまとめる。フロントエンド・バックエンドが別サービスとして分離されているため、この進め方自体は両チーム共通。

## 全体の流れ
1. **APIコントラクトを決める**：[docs/api-contract.yaml](docs/api-contract.yaml)にエンドポイントとJSON形状を先に合意する。ここが決まらないとフロントとバックエンドが並行して進められないため最優先
2. **並行開発**：
   - フロントエンド：[docs/api-contract.yaml](docs/api-contract.yaml)のJSON形状に沿ったモックデータを使ってUIを作る（バックエンドの完成を待たない）。詳細は[frontend/CLAUDE.md](frontend/CLAUDE.md)参照
   - バックエンド：[backend/docs/pipeline](backend/docs/pipeline)の各ステップを実装し、[docs/api-contract.yaml](docs/api-contract.yaml)を満たすAPIを生やす。詳細は[backend/CLAUDE.md](backend/CLAUDE.md)参照
3. **結合**：フロントエンドのモック呼び出しを実APIに差し替える。結合は早めに・小さく行い、最終日にまとめてやらない
4. **磨き込み・デモ準備**：重要度判定の精度、会話形式TTSの間合い、デモ用軽量パイプラインの動作確認など

## ブランチ運用
- `main`ブランチは常に動く状態を保つ
- 機能単位・ステップ単位でブランチを切る。すでにドキュメントを分割している単位にそのまま対応させる：
  - フロントエンド：`frontend/onboarding`、`frontend/home`、`frontend/player`（[frontend/docs/features](frontend/docs/features)参照）
  - バックエンド：`backend/fetch`、`backend/dedupe`、`backend/score`、`backend/script`、`backend/tts`、`backend/api`（[backend/docs/pipeline](backend/docs/pipeline)参照）
- ブランチは短命に。1〜2ステップ分（半日〜1日）で区切ってこまめに`main`へマージする。長生きブランチは差分が膨らみコンフリクトの元になるので避ける
- 担当ファイルが分かれていれば（[Directory structure.md](Directory%20structure.md)参照）、同時並行で作業してもコンフリクトしにくい

## 役割分担の目安
- フロントエンド担当：[frontend/docs/features](frontend/docs/features)配下の画面ごとに1人ずつ割り当てる
- バックエンド担当：[backend/docs/pipeline](backend/docs/pipeline)配下のステップごとに1人ずつ割り当てる（プロバイダ層は差し替え可能な設計にしてあるので、担当者が違っても影響範囲が狭い）

## PRの運用
- 小さく・頻繁に出す。1日1回以上は`main`に統合することを目安にする
- レビューは軽く（「動くか」「他の担当のファイルを壊していないか」を確認する程度）。2.5日の期間で厳密なレビュープロセスは優先度を下げる
- コンフリクトが起きたら早めに解消する（放置すると期限直前に大きなコンフリクトになる）
