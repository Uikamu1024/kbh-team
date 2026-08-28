# CLAUDE.md — プロジェクト指示書

このファイルはClaude Codeがこのリポジトリで作業する際に最初に読み込む前提知識です。詳細な機能要件は `Requirements.md`、AIパイプラインの設計は `Pipeline design.md`、ディレクトリ構成は `Directory structure.md` を参照してください。

## プロジェクト概要
通学中に聞ける、テーマ登録型のパーソナルAIラジオPWA。関西ビギナーズハッカソン vol.8（2.5日開発）向けのプロトタイプ。

## 技術スタック
チーム開発かつハッカソンのため、**無償で完結する構成**を採用する。
- フロントエンド：Next.js（App Router）、PWA対応
- バックエンド：**Next.js API Routes（Route Handlers）**をそのままバックエンドとして使用。別サーバーは立てない
  - 理由：Firebase Cloud Functionsは無料のSparkプランだと外部API（jina.ai/LLM/TTS）への通信ができずBlaze（従量課金）登録が必要になるため回避
- ホスティング：**Vercel（Hobbyプラン・無料）**
- 記事取得：jina.ai Reader（第一候補）／firecrawl free tier（代替）
- LLM：**Gemini API（Google AI Studio 無料枠）**
  - 理由：Claude APIには恒常的な無料枠がなくトライアルクレジットのみのため
- TTS：**VOICEVOX**（無料・オープンソース、キャラクターごとに声が異なるため「2人会話形式」の要件に合致）
  - 開発時はDockerでローカル起動、デモ用はCloud Runの無料枠にVOICEVOX ENGINEをデプロイ想定（Phase 1で早めに動作確認する）
- データ保存：Firebase（Firestore + Storage、Sparkプラン＝無料枠のままでOK。Functionsは使わないため）

## ディレクトリ構成
`Directory structure.md` を参照。開発中に階層が変わりやすいため、CLAUDE.mdとは別ファイルで管理している。

## デザイン方針
参考デザインとして **Spotify** を採用する。
- ダークテーマ基調
- 下部固定ミニプレイヤー→タップで全画面プレイヤーに展開
- 大きなカバーアート＋グラデーション背景
- チャプターリストはプレイリスト的な見せ方（記事1本＝1トラック）
- 波形/プログレスバーのシークUI
ただしSpotifyと異なり「探す・選ぶ」体験（検索・ライブラリ）は不要。ホーム→即再生→チャプター一覧、というシンプルな導線に絞る。

## 開発方針・優先順位
1. **保守性を優先**：記事取得・LLM・TTSは`/lib/providers`配下にプロバイダ単位でファイル分割し、共通インターフェース（`types.ts`）経由で`/lib/pipeline`から呼び出す。TTSやLLMのプロバイダ（VOICEVOXやGeminiなど）を途中で変える可能性があるため、実装差し替え時に他のコードへ影響が及ばないようにする
2. フェーズ分けで進める：
   - Phase 1：1テーマで収集→要約→TTSの一気通貫パイプラインを通す（モックデータでもいい）
   - Phase 2：Firestore/Storage連携、複数テーマ対応
   - Phase 3：プレイヤーUI、1タップ起動対応
   - Phase 4：重要度判定・会話形式TTSなどの磨き込み
3. `REQUIREMENTS.md` の「スコープ外」に書かれた機能は、明示的な指示がない限り実装しない

## 実装上の注意点
- **自動再生制限**：ブラウザはユーザー操作なしの音声自動再生をブロックする。ホーム画面アイコンのタップ→即座に再生開始、という1タップ導線で実現すること（完全な自動再生は不可能な前提で設計する）
- **API制限を考慮**：News API/RSS/LLM/TTSはいずれも呼び出し回数・レイテンシに制約がある。デモ本番用の音声は事前生成し、ライブデモ用には記事数を絞った軽量版パイプラインを別途用意する
- 記事取得元は最初からホワイトリスト化した数サイトに限定し、全サイト対応は行わない

## 環境変数（プレースホルダ）
```
NEXT_PUBLIC_FIREBASE_API_KEY=
NEXT_PUBLIC_FIREBASE_PROJECT_ID=
FIREBASE_STORAGE_BUCKET=
LLM_API_KEY=
TTS_API_KEY=
JINA_AI_API_KEY=
FIRECRAWL_API_KEY=
```

## 現時点の未決定事項（着手前に確認）
- ホワイトリスト対象サイトの最終リスト
- VOICEVOX ENGINEのCloud Run無料枠でのデモ運用が安定するか（Phase 1で検証）