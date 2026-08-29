package llm

import (
	"net/http"
	"os"
	"strings"
)

// FromEnv builds the LLM provider selected by the LLM_PROVIDER environment
// variable ("gemini" (default), "openrouter", or "ollama"). This is the
// single place that switch lives, so cmd/server and any debugging tools stay
// in sync with each other.
func FromEnv(client *http.Client) LLM {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LLM_PROVIDER"))) {
	case "openrouter":
		return NewOpenRouterLLM(client)
	case "ollama":
		return NewOllamaLLM(client)
	default:
		return NewGeminiLLM(client)
	}
}
