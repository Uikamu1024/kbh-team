// Package debuglog is an opt-in verbose logger for local debugging (per-stage
// timing, raw LLM responses on parse failure, etc.). Silent by default;
// set DEBUG_LOG=1 to enable. Production code paths never depend on this
// package's output, so leaving it disabled changes nothing but log volume.
package debuglog

import (
	"log"
	"os"
	"strings"
)

var enabled = isEnabled(os.Getenv("DEBUG_LOG"))

func isEnabled(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "0" && !strings.EqualFold(value, "false")
}

// Printf logs via the standard logger only when DEBUG_LOG is set to a truthy
// value (anything but "", "0", or "false").
func Printf(format string, args ...any) {
	if !enabled {
		return
	}
	log.Printf(format, args...)
}
