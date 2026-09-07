package api

import "net/http"

type generationStatusResponse struct {
	Generating bool                     `json:"generating"`
	Stage      string                   `json:"stage,omitempty"`
	LastError  *generationErrorResponse `json:"lastError,omitempty"`
}

type generationErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// getGenerationStatus is a lightweight, DB-free read of the same in-memory
// state startGenerating/finishGenerating/failGenerating maintain, meant for
// frequent polling (see frontend/src/lib/useGenerateProgram.ts) while a
// regenerate request or batch-cron generation is in flight for this user —
// and, since the actual generation now runs detached from the request that
// started it (see regenerate.go's runRegenerate), this is also how a client
// that reconnects after a page reload or dropped connection learns both
// that it finished and, if it failed, why.
func (s *Server) getGenerationStatus(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	generating, stage, lastError := s.generationStatus(userID)
	response := generationStatusResponse{Generating: generating}
	if generating {
		response.Stage = stage
	}
	if lastError != nil {
		response.LastError = &generationErrorResponse{Code: lastError.code, Message: lastError.message}
	}
	writeJSON(w, http.StatusOK, response)
}
