package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoggingTransport(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/2/collections/1" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"message": "Not allowed."}`))
			return
		}
		_, _ = w.Write([]byte(`{"id": "2"}`))
	}))
	defer srv.Close()
	var log bytes.Buffer
	c, err := New(Options{Host: srv.URL, APIKey: "secret", HTTPClient: &http.Client{Transport: LoggingTransport(nil, &log)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetCollection(t.Context(), "2"); err != nil {
		t.Fatal(err)
	}
	_, err = c.GetCollection(t.Context(), "1")
	// The error body still reaches the client.
	if err == nil || err.Error() != "Not allowed." {
		t.Fatalf("err = %v", err)
	}
	out := log.String()
	for _, want := range []string{"GET " + srv.URL + "/api/2/collections/2 200", "GET " + srv.URL + "/api/2/collections/1 403", `{"message": "Not allowed."}`} {
		if !strings.Contains(out, want) {
			t.Errorf("log misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret") {
		t.Error("API key in the log")
	}
}
