package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"backend/internal/storage"
)

func (s *Server) getAudio(w http.ResponseWriter, r *http.Request) {
	if s.storage == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "音声ストレージが初期化されていません")
		return
	}
	programID := r.PathValue("programId")
	chapterID := r.PathValue("chapterId")
	file, info, err := s.storage.Open(filepath.ToSlash(filepath.Join(programID, chapterID+".wav")))
	if err != nil {
		if errors.Is(err, storage.ErrInvalidID) || errors.Is(err, storage.ErrInvalidPath) || errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "AUDIO_NOT_FOUND", "音声ファイルが見つかりません")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "音声ファイルを開けません")
		return
	}
	defer file.Close()
	seekable, ok := file.(io.ReadSeeker)
	if !ok {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "音声ファイルを配信できません")
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	http.ServeContent(w, r, chapterID+".wav", info.ModTime(), seekable)
}
