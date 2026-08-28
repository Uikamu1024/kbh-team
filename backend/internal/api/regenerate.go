package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"backend/internal/db"
	"backend/internal/domain"
	"backend/internal/pipeline"
)

// regenerateLatestProgram rebuilds and persists a user's latest program.
// The in-memory lock is intentionally process-local, matching the single
// backend process deployment assumed by this project.
func (s *Server) regenerateLatestProgram(w http.ResponseWriter, r *http.Request) {
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

	previousProgram, previousChapters, err := s.database.GetLatestProgramByUser(r.Context(), userID)
	if err != nil {
		s.writeRegenerateLookupError(w, err)
		return
	}

	if !s.startGenerating(userID) {
		writeError(w, http.StatusConflict, "ALREADY_GENERATING", "前回のリクエストを処理中です。しばらく待ってから再度お試しください")
		return
	}
	defer s.finishGenerating(userID)

	today, err := deliveryDate(time.Now(), user.DeliveryTime)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "配信日の計算に失敗しました")
		return
	}
	if _, err := s.database.IncrementResetCount(r.Context(), userID, today); err != nil {
		s.writeResetError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	previousTopics := make([]string, 0, len(previousChapters))
	for _, chapter := range previousChapters {
		previousTopics = append(previousTopics, chapter.Title)
	}
	articles, err := pipeline.FetchArticles(ctx, s.articleFetcher, user.Tags)
	if err != nil {
		writeError(w, http.StatusBadGateway, "UPSTREAM_FETCH_FAILED", "記事の取得に失敗しました")
		return
	}
	topics := pipeline.DedupeArticles(articles)
	selected, changeCount, err := pipeline.ScoreAndSelect(ctx, s.languageModel, topics, user.LengthMinutes, previousTopics)
	if err != nil {
		writeError(w, http.StatusBadGateway, "UPSTREAM_LLM_FAILED", "台本生成に失敗しました")
		return
	}
	greetingText, drafts, err := pipeline.GenerateScript(ctx, s.languageModel, selected)
	if err != nil {
		writeError(w, http.StatusBadGateway, "UPSTREAM_LLM_FAILED", "台本生成に失敗しました")
		return
	}
	chapters, err := pipeline.SynthesizeChapters(ctx, s.speechSynthesizer, greetingText, drafts)
	if err != nil {
		writeError(w, http.StatusBadGateway, "UPSTREAM_TTS_FAILED", "音声生成に失敗しました")
		return
	}
	if s.storage == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "音声ストレージが設定されていません")
		return
	}

	programID, chapterIDs, chapterAudioPaths, err := s.saveGeneratedAudio(chapters)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "生成した音声を保存できません")
		return
	}
	if err := s.database.ReplaceLatestProgramWithIDs(r.Context(), userID, greetingText, changeCount, chapters, chapterAudioPaths, programID, chapterIDs); err != nil {
		_ = s.storage.Delete(programID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "番組を保存できません")
		return
	}
	if err := s.storage.Delete(previousProgram.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "古い番組の音声を削除できません")
		return
	}

	program, storedChapters, err := s.database.GetProgramByID(r.Context(), programID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "生成した番組を取得できません")
		return
	}
	writeJSON(w, http.StatusOK, makeProgramResponse(program, storedChapters))
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

func (s *Server) writeRegenerateLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "指定されたユーザーが見つかりません")
	case errors.Is(err, db.ErrProgramNotFound):
		writeError(w, http.StatusNotFound, "PROGRAM_NOT_FOUND", "作り直す対象の番組がまだ生成されていません")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "作り直す対象の番組を取得できません")
	}
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
