# Codexへの実装依頼メモ（Phase 1）

このファイルは`backend/docs/pipeline/*.md`だけでは決まっていないパッケージ構成の補足と、
Codex（サブエージェント）への依頼を段階分けするための作業指示書。**実装方針の正はあくまで
`backend/docs/pipeline/*.md`・`backend/CLAUDE.md`側**で、ここは「その通りに実装するための
補助線」として使う。

## パッケージ構成の補足決定

`pipeline/*.go`と`providers/*/types.go`が互いを参照すると循環importになるため、
両者が共通で参照する型は`internal/domain`パッケージに置く。

```
/backend
  /internal
    /domain            // Article, Topic, ScoredTopic, ChapterDraft, Line, ChapterAudio
    /providers
      /fetcher
        types.go        // type Fetcher interface { FetchArticles(ctx, tags []string) ([]domain.Article, error) }
        jina.go          // jina.ai Reader実装
        firecrawl.go     // firecrawl free tier実装（代替）
      /llm
        types.go        // type LLM interface { ScoreTopic(...) ; GenerateScript(...) }
        gemini.go
      /tts
        types.go        // type TTS interface { Synthesize(ctx, speaker string, text string) ([]byte, error) }
        voicevox.go
    /pipeline
      fetch.go
      dedupe.go
      score.go
      script.go
      tts.go
  /cmd
    /demo
      main.go            // Phase1のCLIエントリポイント（縮小版パイプラインを1回実行）
```

`internal/pipeline`の各関数はプロバイダの具象型ではなく`internal/providers/*/types.go`の
インターフェースを引数（コンストラクタインジェクション）で受け取ること。`pipeline`パッケージ
自体は`internal/providers/*`をimportしてよい（プロバイダ実装のコンストラクタを呼ぶだけなら）が、
インターフェースを満たしているかの判定は型の構造のみで行われるためimport必須ではない。

### `internal/providers/llm`のインターフェース詳細（docsに明記が無いため補足）
[03-score.md](pipeline/03-score.md)の重要度採点も[04-script.md](pipeline/04-script.md)の台本生成も
同じGemini APIを叩くため、同じ`LLM`インターフェースの別メソッドとして定義する：

```go
type LLM interface {
    // 1件のTopicを採点する。previousTopicsとの類似度からIsNewも判定する
    ScoreTopic(ctx context.Context, topic domain.Topic, previousTopics []string) (score int, isNew bool, err error)
    // 採用済みTopic群から挨拶文とチャプター台本を生成する
    GenerateScript(ctx context.Context, selected []domain.ScoredTopic) (greetingText string, chapters []domain.ChapterDraft, err error)
}
```

### 依存ライブラリの方針
Phase 1（パイプライン一気通貫）の範囲では**Go標準ライブラリのみ**を使う
（`net/http`でjina.ai／firecrawl／Gemini／VOICEVOXいずれもREST呼び出しで足りるため、
公式SDKや外部ルーターライブラリは導入しない）。`go.mod`に新規`require`を追加する場合は
実装前に理由をコメントで明記し、追加してよいか判断に迷ったら実装を止めて報告すること。

### モック方針（外部APIキーが無い環境でも動作確認できるように）
各プロバイダファイル（`jina.go`, `gemini.go`, `voicevox.go`）は環境変数のAPIキーが空の場合、
エラーで落とすのではなく**固定のダミーデータを返すモックモード**で動作させる
（[backend/CLAUDE.md](../CLAUDE.md)の「無償かつローカル完結」方針に沿い、ハッカソン中に
外部APIキー無しでもパイプライン全体の疎通確認ができるようにするため）。モックモードである
ことが分かるよう、返す値に`[MOCK]`等の接頭辞を入れる。

## 作業ステップ（Codexへの依頼単位）

各ステップ完了後、監督側（Claude）がビルド・簡易実行で動作確認してから次に進める。

1. **Step 1**：`internal/domain`の型定義一式 + `internal/providers/{fetcher,llm,tts}/types.go`
   （インターフェース定義のみ、実装ファイルはまだ作らない）
2. **Step 2**：`internal/providers/fetcher/jina.go` + `firecrawl.go` + `internal/pipeline/fetch.go`
3. **Step 3**：`internal/pipeline/dedupe.go`
4. **Step 4**：`internal/providers/llm/gemini.go`（`ScoreTopic`のみ）+ `internal/pipeline/score.go`
5. **Step 5**：`internal/providers/llm/gemini.go`に`GenerateScript`を追加 + `internal/pipeline/script.go`
6. **Step 6**：`internal/providers/tts/voicevox.go` + `internal/pipeline/tts.go`
7. **Step 7**：`cmd/demo/main.go`（環境変数からプロバイダを組み立て、タグをCLI引数で受け取り
   パイプライン全体を実行して結果を標準出力に表示。音声はローカルファイルに書き出す）

## Codexへの共通の禁止事項
- `git`関連のコマンド（`add`/`commit`/`push`等）を実行しない。コミットは監督側が行う
- `backend/`配下以外のファイルを変更しない
- 新規の外部リポジトリのclone・不要なパッケージのインストールをしない
- `go.mod`への新規`require`追加は事前に理由を報告してから
