package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSetsVariablesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# comment\n\nFOO=bar\nQUOTED=\"hello world\"\nSINGLE='value'\nMALFORMED_LINE\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	for _, key := range []string{"FOO", "QUOTED", "SINGLE"} {
		os.Unsetenv(key)
	}

	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := os.Getenv("FOO"); got != "bar" {
		t.Fatalf("FOO = %q, want %q", got, "bar")
	}
	if got := os.Getenv("QUOTED"); got != "hello world" {
		t.Fatalf("QUOTED = %q, want %q", got, "hello world")
	}
	if got := os.Getenv("SINGLE"); got != "value" {
		t.Fatalf("SINGLE = %q, want %q", got, "value")
	}
}

func TestLoadDoesNotOverrideExistingEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("EXISTING=from_file\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("EXISTING", "from_real_env")

	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("EXISTING"); got != "from_real_env" {
		t.Fatalf("EXISTING = %q, want real env value to win", got)
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	if err := Load(filepath.Join(t.TempDir(), "does-not-exist.env")); err != nil {
		t.Fatalf("Load with missing file: %v", err)
	}
}
