# AIパイプライン設計

各ステップの詳細は実装ファイル単位（`/lib/pipeline/*.ts`）で分割し、`docs/pipeline/`配下に置いている。

## 全体フロー
```
①収集 → ②正規化 → ③重複除去 → ④重要度判定 → ⑤要約・台本化 → ⑥音声化 → ⑦保存
```
- [①② 収集・正規化](docs/pipeline/01-fetch.md)
- [③ 重複除去](docs/pipeline/02-dedupe.md)
- [④ 重要度判定](docs/pipeline/03-score.md)
- [⑤ 要約・台本化](docs/pipeline/04-script.md)
- [⑥ 音声化](docs/pipeline/05-tts.md)
- [⑦ 保存](docs/pipeline/06-storage.md)

## デモ用の軽量パイプライン
- 記事数を3件程度に絞った縮小版を別スクリプト（`/scripts`）として用意
- その場でテーマを変更→数十秒〜1分で番組完成、を審査員の前でライブ実演する用途
