// gentrace is a debugging/verification tool: it runs the full article→audio
// pipeline exactly like the demo/generate, regenerate, and batch/run HTTP
// handlers (selecting topics from the articles cache populated by
// cmd/ingest), but with every outbound HTTP request/response (to the LLM
// provider and VOICEVOX) traced and printed live, and saves the full result
// (metadata.json + each chapter's WAV) to backend/debug-output/{timestamp}/
// for later inspection. This is NOT part of the production server; it's a
// standalone tool for manually verifying a provider actually works
// end-to-end without guessing from logs alone. Run cmd/ingest first to
// populate the cache.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/internal/db"
	"backend/internal/domain"
	"backend/internal/envfile"
	"backend/internal/pipeline"
	"backend/internal/providers/llm"
	"backend/internal/providers/tts"
	"backend/internal/trace"
)

const (
	defaultLengthMinutes = 10
	// A full-length program now selects roughly one chapter per 45 assumed
	// seconds (score.go's assumedChapterSeconds), each requiring an LLM
	// script call and several sequential VOICEVOX requests. 120s was only
	// ever enough because a pre-fix bug always produced exactly 1 chapter;
	// budget generously for a real 10+ chapter run.
	pipelineTimeout = 15 * time.Minute
)

func main() {
	log.SetPrefix("gentrace: ")
	if err := run(os.Args[1:]); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if err := envfile.Load(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}

	tags := normalizedTags(args)
	if len(tags) == 0 {
		return fmt.Errorf("provide at least one tag, for example: go run ./cmd/gentrace AI")
	}

	ctx, cancel := context.WithTimeout(context.Background(), pipelineTimeout)
	defer cancel()

	recorder := trace.NewRecorder(nil)
	recorder.Log = printTraceEntry
	tracedClient := recorder.Client()

	languageModel := llm.FromEnv(tracedClient)
	speechSynthesizer := tts.NewVoicevoxTTS(tracedClient)

	fmt.Fprintf(os.Stderr, "=== gentrace: tags=%s provider=%s ===\n", strings.Join(tags, ","), llmProviderName())

	database, err := openDatabase(ctx)
	if err != nil {
		return err
	}
	defer database.Close()

	selected, changeCount, err := pipeline.SelectAndRankCachedTopics(ctx, database, tags, "", defaultLengthMinutes, nil)
	if err != nil {
		if errors.Is(err, pipeline.ErrArticleCacheEmpty) {
			return fmt.Errorf("article cache is empty for tags %v; run `go run ./cmd/ingest` first: %w", tags, err)
		}
		return fmt.Errorf("select cached topics: %w", err)
	}
	log.Printf("selected %d chapters (changeCount=%d)", len(selected), changeCount)

	greetingText, drafts, err := pipeline.GenerateScript(ctx, languageModel, selected)
	if err != nil {
		return fmt.Errorf("generate script: %w", err)
	}
	log.Printf("generated script for %d chapters", len(drafts))

	audioChapters, err := pipeline.SynthesizeChapters(ctx, speechSynthesizer, greetingText, drafts)
	if err != nil {
		return fmt.Errorf("synthesize chapters: %w", err)
	}
	log.Printf("synthesized %d chapters", len(audioChapters))

	outputDir, err := saveResult(tags, greetingText, changeCount, audioChapters, recorder.Entries)
	if err != nil {
		return fmt.Errorf("save result: %w", err)
	}
	fmt.Fprintf(os.Stderr, "=== saved to %s ===\n", outputDir)
	return nil
}

func openDatabase(ctx context.Context) (*db.DB, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set (required for -cache)")
	}
	openCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return db.Open(openCtx, databaseURL)
}

func llmProviderName() string {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROVIDER")))
	if provider == "" {
		return "gemini"
	}
	return provider
}

func printTraceEntry(entry trace.Entry) {
	status := fmt.Sprintf("%d", entry.StatusCode)
	if entry.Error != "" {
		status = "ERROR"
	}
	fmt.Fprintf(os.Stderr, "\n--- [%d] %s %s -> %s (%dms) ---\n", entry.Sequence, entry.Method, entry.URL, status, entry.DurationMs)
	if entry.RequestBody != "" {
		fmt.Fprintf(os.Stderr, "request:  %s\n", entry.RequestBody)
	}
	if entry.Error != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n", entry.Error)
		return
	}
	if entry.ResponseBody != "" {
		fmt.Fprintf(os.Stderr, "response: %s\n", entry.ResponseBody)
	}
}

type metadataChapter struct {
	Position    int    `json:"position"`
	Title       string `json:"title"`
	SourceURL   string `json:"sourceUrl"`
	SourceName  string `json:"sourceName"`
	Script      string `json:"script"`
	DurationSec int    `json:"durationSec"`
	AudioFile   string `json:"audioFile"`
}

type metadata struct {
	GeneratedAt  time.Time         `json:"generatedAt"`
	Tags         []string          `json:"tags"`
	LLMProvider  string            `json:"llmProvider"`
	GreetingText string            `json:"greetingText"`
	ChangeCount  int               `json:"changeCount"`
	Chapters     []metadataChapter `json:"chapters"`
	HTTPTrace    []trace.Entry     `json:"httpTrace"`
}

func saveResult(tags []string, greetingText string, changeCount int, chapters []domain.ChapterAudio, entries []trace.Entry) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	outputDir := filepath.Join("debug-output", timestamp)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}

	meta := metadata{
		GeneratedAt:  time.Now(),
		Tags:         tags,
		LLMProvider:  llmProviderName(),
		GreetingText: greetingText,
		ChangeCount:  changeCount,
		HTTPTrace:    entries,
	}

	for index, chapter := range chapters {
		audioFile := fmt.Sprintf("chapter-%d.wav", index)
		if err := os.WriteFile(filepath.Join(outputDir, audioFile), chapter.AudioBytes, 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", audioFile, err)
		}
		meta.Chapters = append(meta.Chapters, metadataChapter{
			Position:    chapter.Position,
			Title:       chapter.Primary.Title,
			SourceURL:   chapter.Primary.SourceURL,
			SourceName:  chapter.Primary.SourceName,
			Script:      chapterScript(chapter.Lines),
			DurationSec: chapter.DurationSec,
			AudioFile:   audioFile,
		})
	}

	metadataBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "metadata.json"), metadataBytes, 0o644); err != nil {
		return "", err
	}
	return outputDir, nil
}

func chapterScript(lines []domain.Line) string {
	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, fmt.Sprintf("%s: %s", line.Speaker, line.Text))
	}
	return strings.Join(texts, "\n")
}

func normalizedTags(args []string) []string {
	tags := make([]string, 0, len(args))
	for _, arg := range args {
		if tag := strings.TrimSpace(arg); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}
