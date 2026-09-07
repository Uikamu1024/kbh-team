package api

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"backend/internal/db"
	"backend/internal/providers/fetcher"
	"backend/internal/providers/llm"
	"backend/internal/providers/tts"
	"backend/internal/storage"
)

const defaultVoicevoxURL = "http://localhost:50021"

// articleFetchTimeout and languageModelTimeout bound each individual outbound
// HTTP call the on-the-spot generation path makes (jina.ai Reader, the LLM
// provider). A nil *http.Client falls back to http.DefaultClient, which has
// no timeout at all — fine for cmd/ingest's already-bounded background job,
// but on this request/response path an unresponsive upstream would otherwise
// hang the request for the full 10-minute handler context instead of failing
// fast, defeating the "その場で数十秒で生成" design goal.
const (
	articleFetchTimeout  = 30 * time.Second
	languageModelTimeout = 60 * time.Second
)

// Server contains the dependencies shared by API handlers.
type Server struct {
	database          *db.DB
	storage           *storage.Storage
	articleFetcher    fetcher.Fetcher
	languageModel     llm.LLM
	speechSynthesizer tts.TTS
	generatingMu      sync.Mutex
	generatingUsers   map[string]*generationRecord
	voicevoxURL       string
	httpClient        *http.Client
}

// New creates an API server backed by the supplied database and storage.
func New(database *db.DB, fileStorage *storage.Storage) *Server {
	voicevoxURL := strings.TrimRight(strings.TrimSpace(os.Getenv("VOICEVOX_ENGINE_URL")), "/")
	if voicevoxURL == "" {
		voicevoxURL = defaultVoicevoxURL
	}
	languageModel := llm.FromEnv(&http.Client{Timeout: languageModelTimeout})
	return &Server{
		database:          database,
		storage:           fileStorage,
		articleFetcher:    fetcher.NewJinaFetcher(&http.Client{Timeout: articleFetchTimeout}),
		languageModel:     languageModel,
		speechSynthesizer: tts.NewVoicevoxTTS(nil),
		generatingUsers:   make(map[string]*generationRecord),
		voicevoxURL:       voicevoxURL,
		httpClient:        &http.Client{Timeout: 3 * time.Second},
	}
}

// Handler returns the complete HTTP handler for the implemented API routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/users", s.createUser)
	mux.HandleFunc("GET /api/users/{userId}", s.getUser)
	mux.HandleFunc("PUT /api/users/{userId}/tags", s.updateUserTags)
	mux.HandleFunc("PUT /api/users/{userId}/settings", s.updateUserSettings)
	mux.HandleFunc("GET /api/users/{userId}/programs/latest", s.getLatestProgram)
	mux.HandleFunc("POST /api/users/{userId}/programs/latest/regenerate", s.regenerateLatestProgram)
	mux.HandleFunc("GET /api/users/{userId}/generation-status", s.getGenerationStatus)
	mux.HandleFunc("GET /api/users/{userId}/programs", s.listPrograms)
	mux.HandleFunc("GET /api/programs/{programId}", s.getProgram)
	mux.HandleFunc("GET /api/audio/{programId}/{chapterId}", s.getAudio)
	mux.HandleFunc("POST /api/demo/generate", s.generateDemo)
	mux.HandleFunc("POST /api/batch/run", s.runBatch)
	mux.HandleFunc("GET /api/health", s.health)
	return withCORS(mux)
}
