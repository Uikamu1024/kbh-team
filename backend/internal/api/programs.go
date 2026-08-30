package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"backend/internal/db"
)

type programResponse struct {
	ID               string            `json:"id"`
	Title            string            `json:"title"`
	CreatedAt        time.Time         `json:"createdAt"`
	GreetingText     string            `json:"greetingText"`
	ChangeCount      int               `json:"changeCount"`
	TotalDurationSec int               `json:"totalDurationSec"`
	Chapters         []chapterResponse `json:"chapters"`
}

type chapterResponse struct {
	ID          string `json:"id"`
	Position    int    `json:"position"`
	Title       string `json:"title"`
	SourceURL   string `json:"sourceUrl"`
	SourceName  string `json:"sourceName"`
	Script      string `json:"script"`
	AudioURL    string `json:"audioUrl"`
	DurationSec int    `json:"durationSec"`
}

type programHistoryResponse struct {
	Items []programSummaryResponse `json:"items"`
	Total int                      `json:"total"`
}

type programSummaryResponse struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	CreatedAt        time.Time `json:"createdAt"`
	TotalDurationSec int       `json:"totalDurationSec"`
}

func (s *Server) getLatestProgram(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	if s.demoMode {
		programID, assignedAt, err := s.database.GetLatestDemoProgramForUser(r.Context(), userID)
		if err != nil {
			s.writeProgramError(w, err)
			return
		}
		program, chapters, err := s.database.GetProgramByID(r.Context(), programID)
		if err != nil {
			s.writeProgramError(w, err)
			return
		}
		program.CreatedAt = assignedAt
		writeJSON(w, http.StatusOK, makeProgramResponse(program, chapters))
		return
	}

	program, chapters, err := s.database.GetLatestProgramByUser(r.Context(), userID)
	if err != nil {
		s.writeProgramError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, makeProgramResponse(program, chapters))
}

func (s *Server) listPrograms(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		if parsed, err := strconv.Atoi(rawLimit); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 50 {
		limit = 50
	}
	userID := r.PathValue("userId")
	listFunc := s.database.ListProgramsByUser
	if s.demoMode {
		listFunc = s.database.ListDemoProgramsForUser
	}
	summaries, total, err := listFunc(r.Context(), userID, limit)
	if err != nil {
		if errors.Is(err, db.ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "指定されたユーザーが見つかりません")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "番組一覧を取得できません")
		return
	}
	items := make([]programSummaryResponse, 0, len(summaries))
	for _, summary := range summaries {
		items = append(items, programSummaryResponse{
			ID:               summary.ID,
			Title:            summary.Title,
			CreatedAt:        summary.CreatedAt,
			TotalDurationSec: summary.TotalDurationSec,
		})
	}
	writeJSON(w, http.StatusOK, programHistoryResponse{Items: items, Total: total})
}

func (s *Server) getProgram(w http.ResponseWriter, r *http.Request) {
	program, chapters, err := s.database.GetProgramByID(r.Context(), r.PathValue("programId"))
	if err != nil {
		s.writeProgramError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, makeProgramResponse(program, chapters))
}

func (s *Server) writeProgramError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "指定されたユーザーが見つかりません")
	case errors.Is(err, db.ErrProgramNotFound):
		writeError(w, http.StatusNotFound, "PROGRAM_NOT_FOUND", "指定された番組が見つかりません")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "番組を取得できません")
	}
}

func makeProgramResponse(program db.Program, chapters []db.Chapter) programResponse {
	responseChapters := make([]chapterResponse, 0, len(chapters))
	for _, chapter := range chapters {
		responseChapters = append(responseChapters, chapterResponse{
			ID:          chapter.ID,
			Position:    chapter.Position,
			Title:       chapter.Title,
			SourceURL:   chapter.SourceURL,
			SourceName:  chapter.SourceName,
			Script:      chapter.Script,
			AudioURL:    "/api/audio/" + program.ID + "/" + chapter.ID,
			DurationSec: chapter.DurationSec,
		})
	}
	return programResponse{
		ID:               program.ID,
		Title:            program.Title,
		CreatedAt:        program.CreatedAt,
		GreetingText:     program.GreetingText,
		ChangeCount:      program.ChangeCount,
		TotalDurationSec: program.TotalDurationSec,
		Chapters:         responseChapters,
	}
}
