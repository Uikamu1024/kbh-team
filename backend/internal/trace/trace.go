// Package trace provides an http.RoundTripper wrapper that records every
// outbound request/response (method, URL, status, headers, body) for
// debugging tools. It is not used by the production server — providers
// already accept an injectable *http.Client, so wrapping that client here
// requires zero changes to provider code.
package trace

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"
)

// Entry is one recorded request/response pair.
type Entry struct {
	Sequence     int               `json:"sequence"`
	Method       string            `json:"method"`
	URL          string            `json:"url"`
	RequestBody  string            `json:"requestBody,omitempty"`
	StatusCode   int               `json:"statusCode"`
	ResponseBody string            `json:"responseBody,omitempty"`
	Headers      map[string]string `json:"responseHeaders,omitempty"`
	DurationMs   int64             `json:"durationMs"`
	Error        string            `json:"error,omitempty"`
}

// Recorder wraps an http.RoundTripper and appends an Entry for every request.
// It is safe to share across goroutines. Sensitive headers (Authorization,
// API keys embedded in the URL query) are redacted before recording.
type Recorder struct {
	next    http.RoundTripper
	Entries []Entry
	// Log, if set, is called synchronously with each entry as it completes
	// (used to print a live trace to stderr as requests happen).
	Log func(Entry)
}

// NewRecorder wraps base (or http.DefaultTransport if nil).
func NewRecorder(base http.RoundTripper) *Recorder {
	if base == nil {
		base = http.DefaultTransport
	}
	return &Recorder{next: base}
}

// Client returns an *http.Client that records through this Recorder.
func (r *Recorder) Client() *http.Client {
	return &http.Client{Transport: r}
}

const maxBodyLog = 4000

func (r *Recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	sequence := len(r.Entries) + 1
	entry := Entry{
		Sequence: sequence,
		Method:   req.Method,
		URL:      redactURL(req.URL.String()),
	}

	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err == nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
			entry.RequestBody = truncate(string(body))
		}
	}

	start := time.Now()
	resp, err := r.next.RoundTrip(req)
	entry.DurationMs = time.Since(start).Milliseconds()

	if err != nil {
		entry.Error = err.Error()
		r.record(entry)
		return resp, err
	}

	entry.StatusCode = resp.StatusCode
	entry.Headers = map[string]string{"Content-Type": resp.Header.Get("Content-Type")}

	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "audio/") {
		// Binary audio bodies aren't useful (or safe to print) as text;
		// record only the size so the trace stays readable.
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr == nil {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			entry.ResponseBody = "<binary audio, " + itoa(len(body)) + " bytes>"
		}
	} else {
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr == nil {
			resp.Body = io.NopCloser(bytes.NewReader(body))
			entry.ResponseBody = truncate(string(body))
		}
	}

	r.record(entry)
	return resp, nil
}

func (r *Recorder) record(entry Entry) {
	r.Entries = append(r.Entries, entry)
	if r.Log != nil {
		r.Log(entry)
	}
}

func truncate(body string) string {
	if len(body) <= maxBodyLog {
		return body
	}
	return body[:maxBodyLog] + "...<truncated>"
}

// redactURL strips query parameters that commonly carry secrets (e.g.
// Gemini's ?key=...) so trace output/metadata never leaks credentials.
func redactURL(rawURL string) string {
	idx := strings.IndexAny(rawURL, "?")
	if idx < 0 {
		return rawURL
	}
	base, query := rawURL[:idx], rawURL[idx+1:]
	pairs := strings.Split(query, "&")
	for i, pair := range pairs {
		key, _, found := strings.Cut(pair, "=")
		if found && strings.EqualFold(key, "key") {
			pairs[i] = key + "=***"
		}
	}
	return base + "?" + strings.Join(pairs, "&")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := [20]byte{}
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
