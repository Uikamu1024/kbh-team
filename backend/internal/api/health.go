package api

import (
	"context"
	"net/http"
	"strings"
	"time"
)

type healthResponse struct {
	Status   string `json:"status"`
	Postgres string `json:"postgres"`
	Voicevox string `json:"voicevox"`
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	postgresStatus := "ok"
	if s.database == nil || s.database.Ping(r.Context()) != nil {
		postgresStatus = "error"
	}
	voicevoxStatus := "ok"
	if !s.voicevoxHealthy(r.Context()) {
		voicevoxStatus = "error"
	}
	status := "ok"
	httpStatus := http.StatusOK
	if postgresStatus != "ok" || voicevoxStatus != "ok" {
		status = "error"
		httpStatus = http.StatusServiceUnavailable
	}
	writeJSON(w, httpStatus, healthResponse{Status: status, Postgres: postgresStatus, Voicevox: voicevoxStatus})
}

func (s *Server) voicevoxHealthy(parent context.Context) bool {
	if strings.TrimSpace(s.voicevoxURL) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.voicevoxURL+"/version", nil)
	if err != nil {
		return false
	}
	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	return response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices
}
