package llm

import (
	"net/http"
	"os"
	"strings"
)

// Ollama Cloud (https://ollama.com) exposes an OpenAI Chat Completions
// compatible endpoint at /v1 in addition to its native /api endpoint.
// Verified live: https://ollama.com/v1/chat/completions requires
// Authorization: Bearer <OLLAMA_API_KEY> (401 without it, not 404).
const defaultOllamaBaseURL = "https://ollama.com/v1"

// gpt-oss:20b is a comparatively lightweight model among Ollama Cloud's
// available models (see https://ollama.com/v1/models), keeping the default
// request size modest while still usable for JSON-structured scoring/script
// generation.
const defaultOllamaModel = "gpt-oss:20b"

// OllamaLLM is an OpenAI-compatible LLM configured for Ollama Cloud.
type OllamaLLM struct {
	*OpenAICompatibleLLM
}

// NewOllamaLLM creates an Ollama Cloud-backed LLM. Without OLLAMA_API_KEY it
// uses the shared deterministic mock mode (see OpenAICompatibleLLM).
func NewOllamaLLM(client *http.Client) *OllamaLLM {
	model := strings.TrimSpace(os.Getenv("OLLAMA_MODEL"))
	if model == "" {
		model = defaultOllamaModel
	}
	return &OllamaLLM{
		OpenAICompatibleLLM: NewOpenAICompatibleLLM(
			client,
			defaultOllamaBaseURL,
			os.Getenv("OLLAMA_API_KEY"),
			model,
		),
	}
}

var _ LLM = (*OllamaLLM)(nil)
