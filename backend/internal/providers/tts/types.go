package tts

import (
	"context"
)

// TTS synthesizes spoken text for a selected speaker.
type TTS interface {
	Synthesize(ctx context.Context, speaker string, text string) ([]byte, error)
}
