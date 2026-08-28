package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"backend/internal/domain"
)

func TestDatabaseUserAndProgramFlow(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	database, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	userID, err := database.CreateUser(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	defer func() {
		_, _ = database.pool.Exec(ctx, `DELETE FROM chapters WHERE program_id IN (SELECT id FROM programs WHERE user_id = $1::uuid)`, userID)
		_, _ = database.pool.Exec(ctx, `DELETE FROM programs WHERE user_id = $1::uuid`, userID)
		_, _ = database.pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, userID)
	}()

	user, err := database.GetUser(ctx, userID)
	if err != nil {
		t.Fatalf("get created user: %v", err)
	}
	if user.ID != userID || user.DeliveryTime != "06:00" || user.LengthMinutes != 10 {
		t.Fatalf("unexpected default user: %+v", user)
	}

	if err := database.UpdateUserTags(ctx, userID, []string{"AI", "京都"}); err != nil {
		t.Fatalf("update tags: %v", err)
	}
	if err := database.UpdateUserSettings(ctx, userID, "07:30", 15); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	user, err = database.GetUser(ctx, userID)
	if err != nil {
		t.Fatalf("get updated user: %v", err)
	}
	if user.DeliveryTime != "07:30" || user.LengthMinutes != 15 || len(user.Tags) != 2 {
		t.Fatalf("unexpected updated user: %+v", user)
	}

	chapters := []domain.ChapterAudio{
		{
			ChapterDraft: domain.ChapterDraft{
				ScoredTopic: domain.ScoredTopic{
					Topic:           domain.Topic{Primary: domain.Article{Title: "重要なAIニュース", SourceURL: "https://example.com/ai", SourceName: "Example"}},
					ImportanceScore: 5,
					Position:        0,
				},
				Lines: []domain.Line{{Speaker: "A", Text: "本文です。"}},
			},
			DurationSec: 20,
		},
		{
			ChapterDraft: domain.ChapterDraft{
				ScoredTopic: domain.ScoredTopic{
					Topic:           domain.Topic{Primary: domain.Article{Title: "関連ニュース", SourceURL: "https://example.com/related", SourceName: "Example"}},
					ImportanceScore: 3,
					Position:        1,
				},
				Lines: []domain.Line{{Speaker: "B", Text: "関連本文です。"}},
			},
			DurationSec: 30,
		},
	}
	programID, err := database.CreateProgram(ctx, userID, "今日はニュースです。", 1, chapters, []string{"data/1.wav", "data/2.wav"})
	if err != nil {
		t.Fatalf("create program: %v", err)
	}

	program, storedChapters, err := database.GetLatestProgramByUser(ctx, userID)
	if err != nil {
		t.Fatalf("get latest program: %v", err)
	}
	if program.ID != programID || program.Title != "重要なAIニュース" || program.TotalDurationSec != 50 || len(storedChapters) != 2 {
		t.Fatalf("unexpected latest program: %+v, chapters=%+v", program, storedChapters)
	}
	if storedChapters[0].Script != "本文です。" || storedChapters[1].AudioPath != "data/2.wav" {
		t.Fatalf("unexpected stored chapter data: %+v", storedChapters)
	}

	byID, byIDChapters, err := database.GetProgramByID(ctx, programID)
	if err != nil {
		t.Fatalf("get program by ID: %v", err)
	}
	if byID.ID != programID || len(byIDChapters) != 2 {
		t.Fatalf("unexpected program by ID: %+v, chapters=%+v", byID, byIDChapters)
	}

	summaries, total, err := database.ListProgramsByUser(ctx, userID, 10)
	if err != nil {
		t.Fatalf("list programs: %v", err)
	}
	if total != 1 || len(summaries) != 1 || summaries[0].TotalDurationSec != 50 {
		t.Fatalf("unexpected program summaries: total=%d summaries=%+v", total, summaries)
	}

	today := time.Now()
	for expected := 1; expected <= 3; expected++ {
		count, err := database.IncrementResetCount(ctx, userID, today)
		if err != nil || count != expected {
			t.Fatalf("increment reset count %d: count=%d err=%v", expected, count, err)
		}
	}
	if _, err := database.IncrementResetCount(ctx, userID, today); !errors.Is(err, ErrResetLimitExceeded) {
		t.Fatalf("expected reset limit error, got %v", err)
	}
}
