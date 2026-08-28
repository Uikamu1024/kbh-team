package storage

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testProgramID = "123e4567-e89b-12d3-a456-426614174000"
	testChapterID = "123e4567-e89b-12d3-a456-426614174001"
)

func TestStorageSaveOpenAndDelete(t *testing.T) {
	basePath := t.TempDir()
	storage := New(basePath)
	want := []byte("RIFF test audio")

	path, err := storage.Save(testProgramID, testChapterID, want)
	if err != nil {
		t.Fatalf("save audio: %v", err)
	}
	wantPath := filepath.ToSlash(filepath.Join(testProgramID, testChapterID+".wav"))
	if path != wantPath {
		t.Fatalf("expected relative path %q, got %q", wantPath, path)
	}

	file, info, err := storage.Open(path)
	if err != nil {
		t.Fatalf("open audio: %v", err)
	}
	defer file.Close()
	if info.Name() != testChapterID+".wav" || info.Size() != int64(len(want)) {
		t.Fatalf("unexpected file info: name=%q size=%d", info.Name(), info.Size())
	}
	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read audio: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("expected audio %q, got %q", want, got)
	}

	if err := storage.Delete(testProgramID); err != nil {
		t.Fatalf("delete program audio: %v", err)
	}
	if _, err := os.Stat(filepath.Join(basePath, testProgramID)); !os.IsNotExist(err) {
		t.Fatalf("expected program audio directory to be removed, stat error=%v", err)
	}
}

func TestStorageRejectsInvalidIDs(t *testing.T) {
	storage := New(t.TempDir())
	cases := []struct {
		name      string
		programID string
		chapterID string
	}{
		{name: "program traversal", programID: "../escape", chapterID: testChapterID},
		{name: "chapter traversal", programID: testProgramID, chapterID: "../../escape"},
		{name: "short program ID", programID: "not-a-uuid", chapterID: testChapterID},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := storage.Save(testCase.programID, testCase.chapterID, []byte("audio")); err == nil {
				t.Fatal("expected invalid ID error")
			}
		})
	}
}

func TestStorageRejectsTraversalPaths(t *testing.T) {
	storage := New(t.TempDir())
	cases := []string{
		"../secret.wav",
		"/etc/passwd",
		testProgramID + "/../secret.wav",
		testProgramID + "/" + strings.ReplaceAll(testChapterID, "-", "") + ".wav",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			if _, _, err := storage.Open(path); err == nil {
				t.Fatalf("expected invalid path error for %q", path)
			}
		})
	}
}
