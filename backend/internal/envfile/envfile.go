// Package envfile loads simple KEY=VALUE .env files into the process
// environment. Go has no built-in .env support (unlike Node.js), so without
// this, copying .env.example to .env has no effect on os.Getenv.
package envfile

import (
	"bufio"
	"os"
	"strings"
)

// Load reads KEY=VALUE lines from path and sets them as process environment
// variables. Real environment variables that are already set take precedence
// and are left untouched. A missing file is not an error (e.g. CI/production
// environments that configure everything via real env vars).
func Load(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, alreadySet := os.LookupEnv(key); alreadySet {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}
