package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"backend/internal/domain"
	"backend/internal/pipeline"
)

type demoGenerateRequest struct {
	Tags []string `json:"tags"`
}

// generateDemo executes the non-persistent demo pipeline. UUIDs are used for
// the temporary files as well, because Storage deliberately accepts UUID paths
// only; the files can be cleaned up independently of the database.
func (s *Server) generateDemo(w http.ResponseWriter, r *http.Request) {
	var request demoGenerateRequest
	if !decodeJSON(w, r, &request, "INVALID_TAGS", "リクエストJSONの形式が不正です") {
		return
	}
	if len(request.Tags) == 0 {
		writeError(w, http.StatusBadRequest, "INVALID_TAGS", "tagsは1件以上指定してください")
		return
	}
	for index, tag := range request.Tags {
		request.Tags[index] = strings.TrimSpace(tag)
		if request.Tags[index] == "" {
			writeError(w, http.StatusBadRequest, "INVALID_TAGS", "tagsの要素に空文字列は指定できません")
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	articles, err := pipeline.FetchArticles(ctx, s.articleFetcher, request.Tags)
	if err != nil {
		writeError(w, http.StatusBadGateway, "UPSTREAM_FETCH_FAILED", "記事の取得に失敗しました")
		return
	}
	topics := pipeline.DedupeArticles(articles)
	selected, changeCount, err := pipeline.ScoreAndSelect(ctx, s.languageModel, topics, 10, nil)
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

	programID, err := newAPIUUID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "デモ番組のIDを生成できません")
		return
	}
	if s.storage == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "音声ストレージが設定されていません")
		return
	}
	chapterIDs := make([]string, len(chapters))
	for index, chapter := range chapters {
		chapterID, err := newAPIUUID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "デモチャプターのIDを生成できません")
			return
		}
		chapterIDs[index] = chapterID
		if _, err := s.storage.Save(programID, chapterID, chapter.AudioBytes); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "デモ音声を保存できません")
			return
		}
	}

	writeJSON(w, http.StatusOK, makeDemoProgramResponse(programID, greetingText, changeCount, chapters, chapterIDs))
}

func makeDemoProgramResponse(programID, greetingText string, changeCount int, chapters []domain.ChapterAudio, chapterIDs []string) programResponse {
	responseChapters := make([]chapterResponse, 0, len(chapters))
	totalDuration := 0
	for index, chapter := range chapters {
		totalDuration += chapter.DurationSec
		responseChapters = append(responseChapters, chapterResponse{
			ID:              chapterIDs[index],
			Position:        chapter.Position,
			Title:           chapter.Primary.Title,
			SourceURL:       chapter.Primary.SourceURL,
			SourceName:      chapter.Primary.SourceName,
			Script:          demoChapterScript(chapter.Lines),
			AudioURL:        "/api/audio/" + programID + "/" + chapterIDs[index],
			DurationSec:     chapter.DurationSec,
			ImportanceScore: chapter.ImportanceScore,
		})
	}
	title := ""
	if len(chapters) > 0 {
		title = chapters[0].Primary.Title
	}
	return programResponse{
		ID:               programID,
		Title:            title,
		CreatedAt:        time.Now().UTC(),
		GreetingText:     greetingText,
		ChangeCount:      changeCount,
		TotalDurationSec: totalDuration,
		Chapters:         responseChapters,
	}
}

func demoChapterScript(lines []domain.Line) string {
	texts := make([]string, 0, len(lines))
	for _, line := range lines {
		texts = append(texts, line.Text)
	}
	return strings.Join(texts, "\n")
}
