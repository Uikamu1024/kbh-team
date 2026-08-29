package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"backend/internal/db"
	"backend/internal/pipeline"
)

// createAdditionalProgram generates and persists a new program in addition to
// the user's existing latest one, bypassing the once-per-delivery-day limit
// that POST /api/batch/run enforces. It exists purely so a demo can show
// multiple programs in the history list without waiting for multiple days;
// unlike regenerateLatestProgram it does not replace or delete anything.
//
// Ported to the cache-backed selection path (backend/docs/generation/03-selection.md)
// to match regenerateLatestProgram/generateDemo: article selection now reads
// internal/db's cache instead of calling pipeline.FetchArticles/DedupeArticles/
// ScoreAndSelect directly, and the server no longer holds an articleFetcher
// dependency (that lives in cmd/ingest now).
func (s *Server) createAdditionalProgram(w http.ResponseWriter, r *http.Request) {
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

	// regenerateとロックを共有し、同じユーザーの生成が同時に2つ走らないようにする。
	if !s.startGenerating(userID) {
		writeError(w, http.StatusConflict, "ALREADY_GENERATING", "前回のリクエストを処理中です。しばらく待ってから再度お試しください")
		return
	}
	defer s.finishGenerating(userID)

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	previousTopics := []string(nil)
	if _, previousChapters, err := s.database.GetLatestProgramByUser(ctx, userID); err == nil {
		previousTopics = make([]string, 0, len(previousChapters))
		for _, chapter := range previousChapters {
			previousTopics = append(previousTopics, chapter.Title)
		}
	} else if !errors.Is(err, db.ErrProgramNotFound) {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "既存の番組を取得できません")
		return
	}

	selected, changeCount, err := pipeline.SelectAndRankCachedTopics(ctx, s.database, user.Tags, userID, user.LengthMinutes, previousTopics)
	if err != nil {
		if writeArticleSelectionError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "記事キャッシュを取得できません")
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
	if err := s.database.CreateProgramWithIDsAndSeenTopics(ctx, userID, greetingText, changeCount, chapters, chapterAudioPaths, programID, chapterIDs, pipeline.TopicGroupIDs(selected)); err != nil {
		_ = s.storage.Delete(programID)
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "番組を保存できません")
		return
	}

	program, storedChapters, err := s.database.GetProgramByID(r.Context(), programID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "生成した番組を取得できません")
		return
	}
	writeJSON(w, http.StatusCreated, makeProgramResponse(program, storedChapters))
}
