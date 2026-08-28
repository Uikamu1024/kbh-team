package pipeline

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"

	"backend/internal/domain"
	"backend/internal/providers/tts"
)

// SynthesizeChapters adds the greeting to the position-zero chapter for
// synthesis, then joins each chapter's line audio into one WAV file.
func SynthesizeChapters(ctx context.Context, synthesizer tts.TTS, greetingText string, chapters []domain.ChapterDraft) ([]domain.ChapterAudio, error) {
	if synthesizer == nil {
		return nil, fmt.Errorf("TTS is nil")
	}
	if len(chapters) == 0 {
		return nil, nil
	}

	greetingIndex := -1
	for index, chapter := range chapters {
		if chapter.Position == 0 {
			greetingIndex = index
			break
		}
	}

	result := make([]domain.ChapterAudio, 0, len(chapters))
	for chapterIndex, chapter := range chapters {
		lines := append([]domain.Line(nil), chapter.Lines...)
		if chapterIndex == greetingIndex && greetingText != "" {
			lines = append([]domain.Line{{Speaker: "A", Text: greetingText}}, lines...)
		}

		lineAudio := make([][]byte, 0, len(lines))
		for lineIndex, line := range lines {
			audio, err := synthesizer.Synthesize(ctx, line.Speaker, line.Text)
			if err != nil {
				return nil, fmt.Errorf("synthesize chapter %d line %d: %w", chapterIndex, lineIndex, err)
			}
			lineAudio = append(lineAudio, audio)
		}

		audio, format, err := concatenateWAVs(lineAudio)
		if err != nil {
			return nil, fmt.Errorf("combine audio for chapter %d: %w", chapterIndex, err)
		}
		result = append(result, domain.ChapterAudio{
			ChapterDraft: chapter,
			AudioBytes:   audio,
			DurationSec:  wavDurationSeconds(len(format.data), format.byteRate),
		})
	}
	return result, nil
}

type wavFormat struct {
	audioFormat   uint16
	channels      uint16
	sampleRate    uint32
	byteRate      uint32
	blockAlign    uint16
	bitsPerSample uint16
	data          []byte
}

// concatenateWAVs keeps the first WAV's PCM format and joins only data
// chunks. Rebuilding a canonical header avoids carrying offsets or metadata
// from any individual VOICEVOX response into the combined file.
func concatenateWAVs(wavs [][]byte) ([]byte, wavFormat, error) {
	if len(wavs) == 0 {
		format := defaultWAVFormat()
		return encodeWAV(format, nil), format, nil
	}

	var combined wavFormat
	var pcm bytes.Buffer
	for index, audio := range wavs {
		format, err := parseWAV(audio)
		if err != nil {
			return nil, wavFormat{}, fmt.Errorf("parse WAV %d: %w", index, err)
		}
		if index == 0 {
			combined = format
			combined.data = nil
		} else if !sameWAVFormat(combined, format) {
			return nil, wavFormat{}, fmt.Errorf("WAV %d has a different PCM format", index)
		}
		if _, err := pcm.Write(format.data); err != nil {
			return nil, wavFormat{}, err
		}
	}
	if uint64(pcm.Len()) > uint64(math.MaxUint32)-36 {
		return nil, wavFormat{}, fmt.Errorf("combined WAV is too large")
	}
	combined.data = pcm.Bytes()
	return encodeWAV(combined, combined.data), combined, nil
}

func parseWAV(audio []byte) (wavFormat, error) {
	if len(audio) < 12 || string(audio[0:4]) != "RIFF" || string(audio[8:12]) != "WAVE" {
		return wavFormat{}, fmt.Errorf("invalid RIFF/WAVE header")
	}

	var format wavFormat
	foundFormat := false
	foundData := false
	for offset := 12; offset+8 <= len(audio); {
		chunkSize := uint64(binary.LittleEndian.Uint32(audio[offset+4 : offset+8]))
		dataStart := offset + 8
		if chunkSize > uint64(len(audio)-dataStart) {
			return wavFormat{}, fmt.Errorf("WAV chunk exceeds input")
		}
		dataEnd := dataStart + int(chunkSize)
		switch string(audio[offset : offset+4]) {
		case "fmt ":
			if chunkSize < 16 {
				return wavFormat{}, fmt.Errorf("WAV fmt chunk is too short")
			}
			format.audioFormat = binary.LittleEndian.Uint16(audio[dataStart : dataStart+2])
			format.channels = binary.LittleEndian.Uint16(audio[dataStart+2 : dataStart+4])
			format.sampleRate = binary.LittleEndian.Uint32(audio[dataStart+4 : dataStart+8])
			format.byteRate = binary.LittleEndian.Uint32(audio[dataStart+8 : dataStart+12])
			format.blockAlign = binary.LittleEndian.Uint16(audio[dataStart+12 : dataStart+14])
			format.bitsPerSample = binary.LittleEndian.Uint16(audio[dataStart+14 : dataStart+16])
			foundFormat = true
		case "data":
			format.data = append(format.data, audio[dataStart:dataEnd]...)
			foundData = true
		}

		offset = dataEnd
		if chunkSize%2 == 1 {
			offset++
		}
		if offset > len(audio) {
			return wavFormat{}, fmt.Errorf("WAV chunk padding exceeds input")
		}
	}
	if !foundFormat || !foundData {
		return wavFormat{}, fmt.Errorf("WAV is missing fmt or data chunk")
	}
	if format.audioFormat != 1 || format.channels == 0 || format.sampleRate == 0 || format.byteRate == 0 || format.blockAlign == 0 || format.bitsPerSample == 0 {
		return wavFormat{}, fmt.Errorf("WAV is not valid PCM")
	}
	return format, nil
}

func defaultWAVFormat() wavFormat {
	return wavFormat{
		audioFormat:   1,
		channels:      1,
		sampleRate:    22050,
		byteRate:      44100,
		blockAlign:    2,
		bitsPerSample: 16,
	}
}

func encodeWAV(format wavFormat, data []byte) []byte {
	header := make([]byte, 44+len(data))
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+len(data)))
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], format.audioFormat)
	binary.LittleEndian.PutUint16(header[22:24], format.channels)
	binary.LittleEndian.PutUint32(header[24:28], format.sampleRate)
	binary.LittleEndian.PutUint32(header[28:32], format.byteRate)
	binary.LittleEndian.PutUint16(header[32:34], format.blockAlign)
	binary.LittleEndian.PutUint16(header[34:36], format.bitsPerSample)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], uint32(len(data)))
	copy(header[44:], data)
	return header
}

func sameWAVFormat(left, right wavFormat) bool {
	return left.audioFormat == right.audioFormat &&
		left.channels == right.channels &&
		left.sampleRate == right.sampleRate &&
		left.byteRate == right.byteRate &&
		left.blockAlign == right.blockAlign &&
		left.bitsPerSample == right.bitsPerSample
}

func wavDurationSeconds(dataLength int, byteRate uint32) int {
	if byteRate == 0 {
		return 0
	}
	return dataLength / int(byteRate)
}
