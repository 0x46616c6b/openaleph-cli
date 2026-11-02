package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// maxLoggedBody is how much of an error response body is logged.
const maxLoggedBody = 2048

// LoggingTransport logs every request to w: method, URL, status and duration,
// plus the start of the body of error responses. Headers, and with them the
// API key, are not logged, and credentials in URLs (such as the signature of
// a pre-signed storage link a file download is redirected to) are replaced
// by REDACTED. A nil base uses http.DefaultTransport.
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
		t.logf("%s %s error after %s: %v", req.Method, redactURL(req.URL), took, err)
		return nil, err
	}
	if resp.StatusCode < 400 {
		t.logf("%s %s %d %s", req.Method, redactURL(req.URL), resp.StatusCode, took)
		return resp, nil
	}
	// Log the start of the error body and hand the full body on.
	head, _ := io.ReadAll(io.LimitReader(resp.Body, maxLoggedBody))
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(head), resp.Body), resp.Body}
	body := strings.Join(strings.Fields(string(head)), " ")
	t.logf("%s %s %d %s: %s", req.Method, redactURL(req.URL), resp.StatusCode, took, body)
	return resp, nil
}

// secretParams are parts of query parameter names that carry credentials:
// S3 (X-Amz-Signature, X-Amz-Credential, X-Amz-Security-Token), Google
// Cloud Storage (Signature, GoogleAccessId, X-Goog-*), Azure (sig) and
// tokens in general.
var secretParams = []string{"sig", "credential", "token", "key", "secret", "password", "auth", "policy", "googleaccessid"}

// redactURL returns u with the values of credential parameters replaced, and
// without user info.
func redactURL(u *url.URL) string {
	r := *u
	r.User = nil
	if r.RawQuery == "" {
		return r.String()
	}
	q := r.Query()
	for k := range q {
		lk := strings.ToLower(k)
		for _, s := range secretParams {
			if strings.Contains(lk, s) {
				q[k] = []string{"REDACTED"}
				break
			}
		}
	}
	r.RawQuery = q.Encode()
	return r.String()
}
