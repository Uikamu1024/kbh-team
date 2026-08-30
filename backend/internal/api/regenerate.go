package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"backend/internal/db"
	"backend/internal/debuglog"
	"backend/internal/domain"
	"backend/internal/pipeline"
)

// regenerateLatestProgram generates and persists a new program for the user
// ("今日の番組をリセット"). It does NOT replace or delete the existing latest
// program — it adds a new one to the history, the same as
// POST /api/users/{userId}/programs used to (that endpoint was removed as a
// duplicate once this one stopped replacing). The daily reset-count limit
// (IncrementResetCount, 3/day) is the only thing that still makes this
// distinct from an ordinary "generate one more program" call — unless the
// ?bypass query parameter is present, in which case the limit check/counter
// update is skipped entirely. This exists so the onboarding flow's
// first-ever generation (which now calls this endpoint instead of
// POST /api/batch/run, to avoid sweeping every other user's overdue program
// too) doesn't eat into the user's 3/day reset budget before they've had a
// chance to use it. The in-memory lock is intentionally process-local,
// matching the single backend process deployment assumed by this project.
//
// When DEMO_MODE is on, the whole pipeline (select → LLM → TTS) is skipped
// entirely in favor of regenerateLatestProgramDemo, which hands out an
// already-existing program at random instead.
func (s *Server) regenerateLatestProgram(w http.ResponseWriter, r *http.Request) {
	if s.demoMode {
		s.regenerateLatestProgramDemo(w, r)
		return
	}

	userID := r.PathValue("userId")
	user, err := s.database.GetUser(r.Context(), userID)
	if err != nil {
		if errors.Is(err, db.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "指定されたユーザーが見つかりません")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "ユーザーを取得できません")
		return
	}

	// A previous program is optional: previousTopics is just used for
	// IsNew/changeCount diffing, and there being no program yet (first-ever
	// generation) is not an error now that this no longer requires an
	// existing program to replace.
	previousTopics := []string(nil)
	if _, previousChapters, err := s.database.GetLatestProgramByUser(r.Context(), userID); err == nil {
		previousTopics = make([]string, 0, len(previousChapters))
		for _, chapter := range previousChapters {
			previousTopics = append(previousTopics, chapter.Title)
		}
	} else if !errors.Is(err, db.ErrProgramNotFound) {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "既存の番組を取得できません")
		return
	}

	if !s.startGenerating(userID) {
		writeError(w, http.StatusConflict, "ALREADY_GENERATING", "前回のリクエストを処理中です。しばらく待ってから再度お試しください")
		return
	}
	defer s.finishGenerating(userID)

	requestStartedAt := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	stageStartedAt := time.Now()
	selected, changeCount, err := pipeline.SelectAndRankCachedTopics(ctx, s.database, user.Tags, userID, user.LengthMinutes, previousTopics)
	if err != nil {
		debuglog.Printf("api: regenerate: user %s: select topics failed after %s: %v", userID, time.Since(stageStartedAt).Round(time.Millisecond), err)
		if writeArticleSelectionError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "記事キャッシュを取得できません")
		return
	}
	debuglog.Printf("api: regenerate: user %s: selected %d chapters in %s", userID, len(selected), time.Since(stageStartedAt).Round(time.Millisecond))

	if !r.URL.Query().Has("bypass") {
		today, err := deliveryDate(time.Now(), user.DeliveryTime)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "配信日の計算に失敗しました")
			return
		}
		if _, err := s.database.IncrementResetCount(r.Context(), userID, today); err != nil {
			s.writeResetError(w, err)
			return
		}
	}

	stageStartedAt = time.Now()
	greetingText, drafts, err := pipeline.GenerateScript(ctx, s.languageModel, selected)
	if err != nil {
		debuglog.Printf("api: regenerate: user %s: generate script failed after %s: %v", userID, time.Since(stageStartedAt).Round(time.Millisecond), err)
		writeError(w, http.StatusBadGateway, "UPSTREAM_LLM_FAILED", "台本生成に失敗しました")
		return
	}
	debuglog.Printf("api: regenerate: user %s: generated script for %d chapters in %s", userID, len(drafts), time.Since(stageStartedAt).Round(time.Millisecond))

	stageStartedAt = time.Now()
	chapters, err := pipeline.SynthesizeChapters(ctx, s.speechSynthesizer, greetingText, drafts)
	if err != nil {
		debuglog.Printf("api: regenerate: user %s: synthesize chapters failed after %s: %v", userID, time.Since(stageStartedAt).Round(time.Millisecond), err)
		writeError(w, http.StatusBadGateway, "UPSTREAM_TTS_FAILED", "音声生成に失敗しました")
		return
	}
	debuglog.Printf("api: regenerate: user %s: synthesized %d chapters in %s (total so far %s)", userID, len(chapters), time.Since(stageStartedAt).Round(time.Millisecond), time.Since(requestStartedAt).Round(time.Millisecond))
	if s.storage == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "音声ストレージが設定されていません")
		return
	}

	programID, chapterIDs, chapterAudioPaths, err := s.saveGeneratedAudio(chapters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "生成した音声を保存できません")
		return
	}
	if err := s.database.CreateProgramWithIDsAndSeenTopics(r.Context(), userID, greetingText, changeCount, chapters, chapterAudioPaths, programID, chapterIDs, pipeline.TopicGroupIDs(selected)); err != nil {
		_ = s.storage.Delete(programID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "番組を保存できません")
		return
	}

	program, storedChapters, err := s.database.GetProgramByID(r.Context(), programID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "生成した番組を取得できません")
		return
	}
	debuglog.Printf("api: regenerate: user %s: done in %s", userID, time.Since(requestStartedAt).Round(time.Millisecond))
	writeJSON(w, http.StatusCreated, makeProgramResponse(program, storedChapters))
}

// regenerateLatestProgramDemo is DEMO_MODE's stand-in for real generation
// (used by both the onboarding first-ever call and the profile "reset"
// button, since both go through regenerateLatestProgram). It never runs the
// select/LLM/TTS pipeline and never writes to programs/chapters: it picks an
// already-existing program at random — one this user hasn't been handed
// before — and records that assignment in demo_program_assignments
// (backend/internal/db/demo.go). Once every existing program has already
// been assigned to this user, it reuses the real reset path's 429
// RESET_LIMIT_EXCEEDED response, since from the caller's point of view the
// effect is the same ("nothing new right now").
func (s *Server) regenerateLatestProgramDemo(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")

	if !s.startGenerating(userID) {
		writeError(w, http.StatusConflict, "ALREADY_GENERATING", "前回のリクエストを処理中です。しばらく待ってから再度お試しください")
		return
	}
	defer s.finishGenerating(userID)

	programID, assignedAt, err := s.database.AssignRandomDemoProgram(r.Context(), userID)
	if err != nil {
		s.writeResetError(w, err)
		return
	}

	program, chapters, err := s.database.GetProgramByID(r.Context(), programID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "割り当てた番組を取得できません")
		return
	}
	// Displayed as "just generated" for this user, not whenever the
	// underlying program was actually first created in the DB.
	program.CreatedAt = assignedAt
	writeJSON(w, http.StatusCreated, makeProgramResponse(program, chapters))
}

func (s *Server) startGenerating(userID string) bool {
	s.generatingMu.Lock()
	defer s.generatingMu.Unlock()
	if s.generatingUsers == nil {
		s.generatingUsers = make(map[string]struct{})
	}
	if _, exists := s.generatingUsers[userID]; exists {
		return false
	}
	s.generatingUsers[userID] = struct{}{}
	return true
}

func (s *Server) finishGenerating(userID string) {
	s.generatingMu.Lock()
	delete(s.generatingUsers, userID)
	s.generatingMu.Unlock()
}

func (s *Server) saveGeneratedAudio(chapters []domain.ChapterAudio) (string, []string, []string, error) {
	programID, err := newAPIUUID()
	if err != nil {
		return "", nil, nil, err
	}
	chapterIDs := make([]string, len(chapters))
	chapterAudioPaths := make([]string, len(chapters))
	for index, chapter := range chapters {
		chapterID, err := newAPIUUID()
		if err != nil {
			_ = s.storage.Delete(programID)
			return "", nil, nil, err
		}
		path, err := s.storage.Save(programID, chapterID, chapter.AudioBytes)
		if err != nil {
			_ = s.storage.Delete(programID)
			return "", nil, nil, err
		}
		chapterIDs[index] = chapterID
		chapterAudioPaths[index] = path
	}
	return programID, chapterIDs, chapterAudioPaths, nil
}

func (s *Server) writeResetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "指定されたユーザーが見つかりません")
	case errors.Is(err, db.ErrResetLimitExceeded):
		writeError(w, http.StatusTooManyRequests, "RESET_LIMIT_EXCEEDED", "本日の作り直し回数の上限（3回）に達しています")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "作り直し回数を更新できません")
	}
}

func deliveryDate(now time.Time, deliveryTime string) (time.Time, error) {
	delivery, err := time.Parse("15:04", deliveryTime)
	if err != nil {
		return time.Time{}, err
	}
	currentMinutes := now.Hour()*60 + now.Minute()
	deliveryMinutes := delivery.Hour()*60 + delivery.Minute()
	if currentMinutes < deliveryMinutes {
		now = now.AddDate(0, 0, -1)
	}
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), nil
}
