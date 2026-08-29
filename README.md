# Daybrief（通学ラジオ）

> 通学中に聴ける、興味タグ登録型のパーソナルAIニュースラジオ

## デモ

<!-- スクリーンショット or GIF をここに貼る。動くものが一番強い -->

- デモURL：（完全ローカル構成のため無し。発表者PCでのライブ実演を想定）
- 動画：

## 課題

通学・通勤中に情報収集したいが、画面を見続けるのは難しい学生・社会人は多い。ニュースやSNS、ブログなど情報源が分散していて、興味のあるテーマだけを効率よく追うのが手間になっている。

## 解決方法

選んだテーマをもとにAIが記事を収集・要約し、5〜15分の音声番組として毎朝配信する。

- 興味タグを登録するだけで、記事の収集から要約・音声化までを自動化
- ホーム画面をワンタップするだけで再生開始（検索・選択の手間ゼロ）
- 5〜15分、通学・通勤時間にちょうど良い長さに自動調整

## 使用技術

| 領域 | 技術 |
| --- | --- |
| フロントエンド | React + Vite（PWA）、react-router-dom、Tailwind CSS v4 |
| バックエンド | Go（標準net/http）、PostgreSQL、ローカルファイルストレージ |
| その他 | jina.ai Reader／firecrawl（記事取得）、Gemini API／OpenRouter（LLM）、VOICEVOX（TTS）、Docker Compose |

## 動かし方

```bash
docker compose up -d

cd backend && cp .env.example .env
go run ./cmd/server

cd frontend && npm install && cp .env.example .env.local
npm run dev
```

初めての人向けの詳しいセットアップ手順（Docker Desktopのインストールから）は[README_detailed.md](README_detailed.md)を参照してください。

## チーム

| 役割 | 名前 | GitHub |
| --- | --- | --- |
|  | Seiya |  |
|  | Sorato |  |
|  | Ryuuki |  |

---

関西ビギナーズハッカソン vol.8 (2026/08/28-30)
