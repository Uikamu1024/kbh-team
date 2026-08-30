package api

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"backend/internal/db"
	"backend/internal/providers/llm"
	"backend/internal/providers/tts"
	"backend/internal/storage"
)

const defaultVoicevoxURL = "http://localhost:50021"

// Server contains the dependencies shared by API handlers.
type Server struct {
	database          *db.DB
	storage           *storage.Storage
	languageModel     llm.LLM
	speechSynthesizer tts.TTS
	generatingMu      sync.Mutex
	generatingUsers   map[string]struct{}
	voicevoxURL       string
	httpClient        *http.Client
	demoMode          bool
}

// New creates an API server backed by the supplied database and storage.
func New(database *db.DB, fileStorage *storage.Storage) *Server {
	voicevoxURL := strings.TrimRight(strings.TrimSpace(os.Getenv("VOICEVOX_ENGINE_URL")), "/")
	if voicevoxURL == "" {
		voicevoxURL = defaultVoicevoxURL
	}
	languageModel := llm.FromEnv(nil)
	return &Server{
		database:          database,
		storage:           fileStorage,
		languageModel:     languageModel,
		speechSynthesizer: tts.NewVoicevoxTTS(nil),
		generatingUsers:   make(map[string]struct{}),
		voicevoxURL:       voicevoxURL,
		httpClient:        &http.Client{Timeout: 3 * time.Second},
		demoMode:          isTruthyEnv(os.Getenv("DEMO_MODE")),
	}
}

// isTruthyEnv reports whether a boolean-flag environment variable is set to
// anything other than "", "0", or "false" (mirrors internal/debuglog's
// convention for the same kind of on/off env var).
func isTruthyEnv(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "0" && !strings.EqualFold(value, "false")
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
	mux.HandleFunc("GET /api/users/{userId}/programs", s.listPrograms)
	mux.HandleFunc("GET /api/programs/{programId}", s.getProgram)
	mux.HandleFunc("GET /api/audio/{programId}/{chapterId}", s.getAudio)
	mux.HandleFunc("POST /api/demo/generate", s.generateDemo)
	mux.HandleFunc("POST /api/batch/run", s.runBatch)
	mux.HandleFunc("GET /api/health", s.health)
	return withCORS(mux)
}
