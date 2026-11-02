package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxLoggedBody is how much of an error response body is logged.
const maxLoggedBody = 2048

// LoggingTransport logs every request to w: method, URL, status and duration,
// plus the start of the body of error responses. Headers, and with them the
// API key, are not logged. A nil base uses http.DefaultTransport.
func LoggingTransport(base http.RoundTripper, w io.Writer) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &loggingTransport{base: base, w: w}
}

type loggingTransport struct {
	base http.RoundTripper
	mu   sync.Mutex
	w    io.Writer
}

func (t *loggingTransport) logf(format string, args ...any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintf(t.w, time.Now().Format("2006-01-02T15:04:05.000 ")+format+"\n", args...)
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	took := time.Since(start).Round(time.Millisecond)
	if err != nil {
		t.logf("%s %s error after %s: %v", req.Method, req.URL, took, err)
		return nil, err
	}
	if resp.StatusCode < 400 {
		t.logf("%s %s %d %s", req.Method, req.URL, resp.StatusCode, took)
		return resp, nil
	}
	// Log the start of the error body and hand the full body on.
	head, _ := io.ReadAll(io.LimitReader(resp.Body, maxLoggedBody))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), resp.Body), resp.Body}
	body := strings.Join(strings.Fields(string(head)), " ")
	t.logf("%s %s %d %s: %s", req.Method, req.URL, resp.StatusCode, took, body)
	return resp, nil
}
