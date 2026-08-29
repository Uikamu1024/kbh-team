package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"backend/internal/db"
	"backend/internal/debuglog"
	"backend/internal/pipeline"
)

type batchRunResponse struct {
	AcceptedAt time.Time `json:"acceptedAt"`
}

// runBatch acknowledges immediately; generation continues independently of
// the request context because the HTTP client is not expected to wait for it.
func (s *Server) runBatch(w http.ResponseWriter, r *http.Request) {
	acceptedAt := time.Now().UTC()
	writeJSON(w, http.StatusAccepted, batchRunResponse{AcceptedAt: acceptedAt})

	go func() {
		s.processBatch()
	}()
}

func (s *Server) processBatch() {
	usersContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	users, err := s.database.ListAllUsers(usersContext)
	cancel()
	if err != nil {
		log.Printf("batch: list users failed: %v", err)
		return
	}

	for _, user := range users {
		s.processBatchUser(user)
	}
}

func (s *Server) processBatchUser(user db.User) {
	now := time.Now()
	if !deliveryTimeReached(now, user.DeliveryTime) {
		return
	}
	since, err := deliveryDate(now, user.DeliveryTime)
	if err != nil {
		log.Printf("batch: user %s has invalid delivery time %q: %v", user.ID, user.DeliveryTime, err)
		return
	}

	checkContext, cancelCheck := context.WithTimeout(context.Background(), 10*time.Second)
	hasProgram, err := s.database.HasProgramSince(checkContext, user.ID, since)
	cancelCheck()
	if err != nil {
		log.Printf("batch: check today's program for user %s failed: %v", user.ID, err)
		return
	}
	if hasProgram {
		return
	}
	if !s.startGenerating(user.ID) {
		log.Printf("batch: skip user %s because generation is already running", user.ID)
		return
	}
	defer s.finishGenerating(user.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	previousTopics := []string(nil)
	if _, previousChapters, err := s.database.GetLatestProgramByUser(ctx, user.ID); err == nil {
		previousTopics = make([]string, 0, len(previousChapters))
		for _, chapter := range previousChapters {
			previousTopics = append(previousTopics, chapter.Title)
		}
	} else if !errors.Is(err, db.ErrProgramNotFound) {
		log.Printf("batch: get previous program for user %s failed: %v", user.ID, err)
		return
	}

	requestStartedAt := time.Now()
	stageStartedAt := time.Now()
	selected, changeCount, err := pipeline.SelectAndRankCachedTopics(ctx, s.database, user.Tags, user.ID, user.LengthMinutes, previousTopics)
	if err != nil {
		if errors.Is(err, pipeline.ErrArticleCacheEmpty) || errors.Is(err, pipeline.ErrNoUnseenArticles) {
			log.Printf("batch: skip user %s: %v", user.ID, err)
			return
		}
		log.Printf("batch: select cached articles for user %s failed after %s: %v", user.ID, time.Since(stageStartedAt).Round(time.Millisecond), err)
		return
	}
	debuglog.Printf("batch: user %s: selected %d chapters in %s", user.ID, len(selected), time.Since(stageStartedAt).Round(time.Millisecond))

	stageStartedAt = time.Now()
	greetingText, drafts, err := pipeline.GenerateScript(ctx, s.languageModel, selected)
	if err != nil {
		log.Printf("batch: generate script for user %s failed after %s: %v", user.ID, time.Since(stageStartedAt).Round(time.Millisecond), err)
		return
	}
	debuglog.Printf("batch: user %s: generated script for %d chapters in %s", user.ID, len(drafts), time.Since(stageStartedAt).Round(time.Millisecond))

	stageStartedAt = time.Now()
	chapters, err := pipeline.SynthesizeChapters(ctx, s.speechSynthesizer, greetingText, drafts)
	if err != nil {
		log.Printf("batch: synthesize chapters for user %s failed after %s: %v", user.ID, time.Since(stageStartedAt).Round(time.Millisecond), err)
		return
	}
	debuglog.Printf("batch: user %s: synthesized %d chapters in %s (total so far %s)", user.ID, len(chapters), time.Since(stageStartedAt).Round(time.Millisecond), time.Since(requestStartedAt).Round(time.Millisecond))
	if s.storage == nil {
		log.Printf("batch: audio storage is not configured for user %s", user.ID)
		return
	}

	programID, chapterIDs, chapterAudioPaths, err := s.saveGeneratedAudio(chapters)
	if err != nil {
		log.Printf("batch: save audio for user %s failed: %v", user.ID, err)
		return
	}
	if err := s.database.CreateProgramWithIDsAndSeenTopics(ctx, user.ID, greetingText, changeCount, chapters, chapterAudioPaths, programID, chapterIDs, pipeline.TopicGroupIDs(selected)); err != nil {
		_ = s.storage.Delete(programID)
		log.Printf("batch: save program for user %s failed: %v", user.ID, err)
		return
	}
	log.Printf("batch: generated program %s for user %s in %s", programID, user.ID, time.Since(requestStartedAt).Round(time.Millisecond))
}

func deliveryTimeReached(now time.Time, deliveryTime string) bool {
	delivery, err := time.Parse("15:04", deliveryTime)
	if err != nil {
		return false
	}
	return now.Hour()*60+now.Minute() >= delivery.Hour()*60+delivery.Minute()
}
