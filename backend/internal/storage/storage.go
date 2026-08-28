// Package storage provides local filesystem storage for generated chapter audio.
package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrInvalidID indicates that a program or chapter identifier is not a UUID.
	ErrInvalidID = errors.New("invalid UUID")
	// ErrInvalidPath indicates that a path is not a permitted relative chapter path.
	ErrInvalidPath = errors.New("invalid audio path")
)

// Storage manages generated audio below a configured base directory.
type Storage struct {
	basePath string
}

// New creates local filesystem storage rooted at basePath.
func New(basePath string) *Storage {
	return &Storage{basePath: basePath}
}

// Save writes chapter audio to {basePath}/{programID}/{chapterID}.wav and
// returns the relative path used by chapters.audio_path.
func (s *Storage) Save(programID, chapterID string, audioBytes []byte) (string, error) {
	if !isUUID(programID) || !isUUID(chapterID) {
		return "", ErrInvalidID
	}

	relativePath := filepath.Join(programID, chapterID+".wav")
	directory := filepath.Join(s.basePath, programID)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("create audio directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.basePath, relativePath), audioBytes, 0o644); err != nil {
		return "", fmt.Errorf("write audio file: %w", err)
	}
	return filepath.ToSlash(relativePath), nil
}

// Open opens a previously saved relative audio path and returns its file info.
func (s *Storage) Open(path string) (io.ReadCloser, os.FileInfo, error) {
	relativePath, err := validateAudioPath(path)
	if err != nil {
		return nil, nil, err
	}

	file, err := os.Open(filepath.Join(s.basePath, filepath.FromSlash(relativePath)))
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, info, nil
}

// Delete removes all audio belonging to a program. Removing a missing
// directory is intentionally treated as success for idempotent cleanup.
func (s *Storage) Delete(programID string) error {
	if !isUUID(programID) {
		return ErrInvalidID
	}
	if err := os.RemoveAll(filepath.Join(s.basePath, programID)); err != nil {
		return fmt.Errorf("delete program audio: %w", err)
	}
	return nil
}

func validateAudioPath(path string) (string, error) {
	// Normalize both separators before checking so this remains safe if a path
	// arrives from a different platform or through an HTTP request.
	normalized := strings.ReplaceAll(path, "\\", "/")
	if normalized == "" || strings.HasPrefix(normalized, "/") || strings.ContainsRune(normalized, '\x00') {
		return "", ErrInvalidPath
	}
	parts := strings.Split(normalized, "/")
	if len(parts) != 2 || !isUUID(parts[0]) || !strings.HasSuffix(parts[1], ".wav") || !isUUID(strings.TrimSuffix(parts[1], ".wav")) {
		return "", ErrInvalidPath
	}
	return filepath.ToSlash(filepath.Join(parts[0], parts[1])), nil
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !isHex(character) {
			return false
		}
	}
	return true
}

func isHex(character rune) bool {
	return character >= '0' && character <= '9' ||
		character >= 'a' && character <= 'f' ||
		character >= 'A' && character <= 'F'
}
