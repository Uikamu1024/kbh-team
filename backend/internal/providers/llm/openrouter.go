package llm

import (
	"net/http"
	"os"
	"strings"
)

const defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"

// The :free suffix selects OpenRouter's free variant of this model, keeping
// the default setup usable without paid model credits.
const defaultOpenRouterModel = "meta-llama/llama-3.1-8b-instruct:free"

// OpenRouterLLM is an OpenAI-compatible LLM configured for OpenRouter.
type OpenRouterLLM struct {
	*OpenAICompatibleLLM
}

// NewOpenRouterLLM creates an OpenRouter-backed LLM. Without
// OPENROUTER_API_KEY it uses the shared deterministic mock mode.
func NewOpenRouterLLM(client *http.Client) *OpenRouterLLM {
	model := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
	if model == "" {
		model = defaultOpenRouterModel
	}
	return &OpenRouterLLM{
		OpenAICompatibleLLM: NewOpenAICompatibleLLM(
			client,
			defaultOpenRouterBaseURL,
			os.Getenv("OPENROUTER_API_KEY"),
			model,
		),
	}
}

var _ LLM = (*OpenRouterLLM)(nil)
