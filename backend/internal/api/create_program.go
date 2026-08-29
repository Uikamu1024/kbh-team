package api

import (
	"context"
	"errors"
	"log"
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

	articles, err := pipeline.FetchArticles(ctx, s.articleFetcher, user.Tags)
	if err != nil {
		log.Printf("api: create additional program: fetch articles: %v", err)
		writeError(w, http.StatusBadGateway, "UPSTREAM_FETCH_FAILED", "記事の取得に失敗しました")
		return
	}
	topics := pipeline.DedupeArticles(articles)
	selected, changeCount, err := pipeline.ScoreAndSelect(ctx, s.languageModel, topics, user.LengthMinutes, previousTopics)
	if err != nil {
		log.Printf("api: create additional program: score topics: %v", err)
		writeError(w, http.StatusBadGateway, "UPSTREAM_LLM_FAILED", "台本生成に失敗しました")
		return
	}
	greetingText, drafts, err := pipeline.GenerateScript(ctx, s.languageModel, selected)
	if err != nil {
		log.Printf("api: create additional program: generate script: %v", err)
		writeError(w, http.StatusBadGateway, "UPSTREAM_LLM_FAILED", "台本生成に失敗しました")
		return
	}
	chapters, err := pipeline.SynthesizeChapters(ctx, s.speechSynthesizer, greetingText, drafts)
	if err != nil {
		log.Printf("api: create additional program: synthesize chapters: %v", err)
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
	if err := s.database.CreateProgramWithIDs(ctx, userID, greetingText, changeCount, chapters, chapterAudioPaths, programID, chapterIDs); err != nil {
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
