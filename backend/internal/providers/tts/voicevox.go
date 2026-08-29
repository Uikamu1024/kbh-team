package tts

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// speakerIDs is a provisional mapping and can be tuned later as the program's
// character casting is decided.
var speakerIDs = map[string]int{
	"A": 3,
	"B": 8,
}

// voiceSettings can be tuned independently for each speaker.
// speedScale: 1.0 is the default, values above 1.0 are faster.
// intonationScale: 1.0 is the default, values above 1.0 add emphasis.
var voiceSettings = map[string]struct {
	SpeedScale      float64
	PitchScale      float64
	IntonationScale float64
	VolumeScale     float64
}{
	"A": {SpeedScale: 1.04, PitchScale: 0.0, IntonationScale: 1.08, VolumeScale: 1.0},
	"B": {SpeedScale: 0.97, PitchScale: 0.0, IntonationScale: 1.08, VolumeScale: 1.0},
}

// VoicevoxTTS synthesizes speech through the local VOICEVOX ENGINE API.
type VoicevoxTTS struct {
	client *http.Client
}

// NewVoicevoxTTS creates a VOICEVOX-backed TTS. A nil client uses the default
// HTTP client.
func NewVoicevoxTTS(client *http.Client) *VoicevoxTTS {
	if client == nil {
		client = http.DefaultClient
	}
	return &VoicevoxTTS{client: client}
}

// Synthesize performs VOICEVOX's audio_query and synthesis requests. If the
// engine is not configured or unavailable, it returns a valid silent WAV so
// local pipeline execution can continue without VOICEVOX running.
func (v *VoicevoxTTS) Synthesize(ctx context.Context, speaker string, text string) ([]byte, error) {
	speakerID, ok := speakerIDs[speaker]
	if !ok {
		return nil, fmt.Errorf("unknown speaker %q", speaker)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("VOICEVOX_ENGINE_URL")), "/")
	if baseURL == "" {
		return silentWAV(), nil
	}

	queryURL := baseURL + "/audio_query?" + url.Values{
		"speaker": []string{strconv.Itoa(speakerID)},
		"text":    []string{text},
	}.Encode()
	queryRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, queryURL, nil)
	if err != nil {
		return engineFallback(ctx, err)
	}

	queryResponse, err := v.httpClient().Do(queryRequest)
	if err != nil {
		return engineFallback(ctx, err)
	}
	queryBody, err := readSuccessfulResponse(queryResponse, "audio_query")
	if err != nil {
		return engineFallback(ctx, err)
	}
	queryBody, err = tuneVoiceQuery(queryBody, speaker)
	if err != nil {
		return engineFallback(ctx, err)
	}

	synthesisURL := baseURL + "/synthesis?" + url.Values{
		"speaker": []string{strconv.Itoa(speakerID)},
	}.Encode()
	synthesisRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, synthesisURL, bytes.NewReader(queryBody))
	if err != nil {
		return engineFallback(ctx, err)
	}
	synthesisRequest.Header.Set("Content-Type", "application/json")

	synthesisResponse, err := v.httpClient().Do(synthesisRequest)
	if err != nil {
		return engineFallback(ctx, err)
	}
	audio, err := readSuccessfulResponse(synthesisResponse, "synthesis")
	if err != nil {
		return engineFallback(ctx, err)
	}
	if !isWAV(audio) {
		return engineFallback(ctx, errors.New("VOICEVOX synthesis response is not a WAV"))
	}
	return audio, nil
}

func tuneVoiceQuery(queryBody []byte, speaker string) ([]byte, error) {
	settings, ok := voiceSettings[speaker]
	if !ok {
		return queryBody, nil
	}

	var query map[string]any
	if err := json.Unmarshal(queryBody, &query); err != nil {
		return nil, fmt.Errorf("decode VOICEVOX audio query: %w", err)
	}
	query["speedScale"] = settings.SpeedScale
	query["pitchScale"] = settings.PitchScale
	query["intonationScale"] = settings.IntonationScale
	query["volumeScale"] = settings.VolumeScale
	return json.Marshal(query)
}

func (v *VoicevoxTTS) httpClient() *http.Client {
	if v.client == nil {
		return http.DefaultClient
	}
	return v.client
}

func readSuccessfulResponse(response *http.Response, operation string) ([]byte, error) {
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read VOICEVOX %s response: %w", operation, err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("VOICEVOX %s returned status %d: %s", operation, response.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func engineFallback(ctx context.Context, _ error) ([]byte, error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	return silentWAV(), nil
}

func silentWAV() []byte {
	return makeWAVHeader(22050, 1, 16, nil)
}

func isWAV(audio []byte) bool {
	return len(audio) >= 12 && string(audio[0:4]) == "RIFF" && string(audio[8:12]) == "WAVE"
}

func makeWAVHeader(sampleRate uint32, channels uint16, bitsPerSample uint16, data []byte) []byte {
	const headerSize = 44
	header := make([]byte, headerSize+len(data))
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+len(data)))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], channels)
	binary.LittleEndian.PutUint32(header[24:28], sampleRate)
	byteRate := sampleRate * uint32(channels) * uint32(bitsPerSample) / 8
	binary.LittleEndian.PutUint32(header[28:32], byteRate)
	blockAlign := channels * bitsPerSample / 8
	binary.LittleEndian.PutUint16(header[32:34], blockAlign)
	binary.LittleEndian.PutUint16(header[34:36], bitsPerSample)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(len(data)))
	copy(header[44:], data)
	return header
}

var _ TTS = (*VoicevoxTTS)(nil)
