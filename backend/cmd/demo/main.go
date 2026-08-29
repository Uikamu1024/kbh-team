package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/internal/domain"
	"backend/internal/envfile"
	"backend/internal/pipeline"
	"backend/internal/providers/fetcher"
	"backend/internal/providers/llm"
	"backend/internal/providers/tts"
)

const (
	defaultLengthMinutes = 10
	demoTimeout          = 120 * time.Second
)

func main() {
	log.SetPrefix("demo: ")
	if err := run(os.Args[1:]); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Go has no built-in .env support; without this, a copied .env.example
	// (e.g. for a real Gemini/jina API key) would silently have no effect.
	if err := envfile.Load(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}

	tags := normalizedTags(args)
	if len(tags) == 0 {
		return fmt.Errorf("provide at least one tag, for example: go run ./cmd/demo AI")
	}

	ctx, cancel := context.WithTimeout(context.Background(), demoTimeout)
	defer cancel()

	// FETCHER_PROVIDER-based switching is planned for the future; Jina is the
	// default provider for this Phase 1 CLI.
	articleFetcher := fetcher.NewJinaFetcher(nil)
	languageModel := llm.NewGeminiLLM(nil)
	speechSynthesizer := tts.NewVoicevoxTTS(nil)
	previousTopics := []string{}

	log.Printf("fetching articles for tags: %s", strings.Join(tags, ", "))
	articles, err := pipeline.FetchArticles(ctx, articleFetcher, tags)
	if err != nil {
		return fmt.Errorf("fetch articles: %w", err)
	}
	log.Printf("fetched %d articles", len(articles))

	topics := pipeline.DedupeArticles(articles)
	log.Printf("deduplicated into %d topics", len(topics))

	selected, changeCount, err := pipeline.ScoreAndSelect(ctx, languageModel, topics, defaultLengthMinutes, previousTopics)
	if err != nil {
		return fmt.Errorf("score and select topics: %w", err)
	}
	log.Printf("selected %d chapters (changeCount=%d)", len(selected), changeCount)

	greetingText, chapterDrafts, err := pipeline.GenerateScript(ctx, languageModel, selected)
	if err != nil {
		return fmt.Errorf("generate script: %w", err)
	}
	log.Printf("generated script for %d chapters", len(chapterDrafts))

	audioChapters, err := pipeline.SynthesizeChapters(ctx, speechSynthesizer, greetingText, chapterDrafts)
	if err != nil {
		return fmt.Errorf("synthesize chapters: %w", err)
	}
	log.Printf("synthesized %d chapters", len(audioChapters))

	if err := saveAudio(audioChapters); err != nil {
		return fmt.Errorf("save audio: %w", err)
	}

	printResult(greetingText, changeCount, audioChapters)
	return nil
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

func saveAudio(chapters []domain.ChapterAudio) error {
	const audioDirectory = "data/audio/demo"
	if err := os.MkdirAll(audioDirectory, 0o755); err != nil {
		return err
	}
	for index, chapter := range chapters {
		path := filepath.Join(audioDirectory, fmt.Sprintf("%03d.wav", index+1))
		if err := os.WriteFile(path, chapter.AudioBytes, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		log.Printf("saved chapter %d audio to %s", index+1, path)
	}
	return nil
}

func printResult(greetingText string, changeCount int, chapters []domain.ChapterAudio) {
	fmt.Printf("greetingText: %s\n", greetingText)
	fmt.Printf("changeCount: %d\n", changeCount)
	for index, chapter := range chapters {
		lines := make([]string, 0, len(chapter.Lines))
		for _, line := range chapter.Lines {
			lines = append(lines, line.Text)
		}
		fmt.Printf("\nChapter %d\n", index+1)
		fmt.Printf("Title: %s\n", chapter.Primary.Title)
		fmt.Printf("ImportanceScore: %d\n", chapter.ImportanceScore)
		fmt.Printf("DurationSec: %d\n", chapter.DurationSec)
		fmt.Printf("Script:\n%s\n", strings.Join(lines, "\n"))
	}
}
