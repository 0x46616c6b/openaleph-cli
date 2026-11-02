package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestRedactURL(t *testing.T) {
	cases := map[string]string{
		"https://s3.example.org/b/data?X-Amz-Algorithm=AWS4&X-Amz-Credential=AKIA%2F1&X-Amz-Expires=600&X-Amz-Signature=abc&X-Amz-Security-Token=t": "https://s3.example.org/b/data?X-Amz-Algorithm=AWS4&X-Amz-Credential=REDACTED&X-Amz-Expires=600&X-Amz-Security-Token=REDACTED&X-Amz-Signature=REDACTED",
		"https://storage.googleapis.com/b/o?GoogleAccessId=x&Signature=y&Expires=1":                                                                 "https://storage.googleapis.com/b/o?Expires=1&GoogleAccessId=REDACTED&Signature=REDACTED",
		"https://aleph.example.org/api/2/archive?token=jwt&file_name=a.pdf":                                                                         "https://aleph.example.org/api/2/archive?file_name=a.pdf&token=REDACTED",
		"https://user:pw@aleph.example.org/api/2/entities?q=acme&offset=50":                                                                         "https://aleph.example.org/api/2/entities?offset=50&q=acme",
		"https://aleph.example.org/api/2/collections/7":                                                                                             "https://aleph.example.org/api/2/collections/7",
	}
	for in, want := range cases {
		u, err := url.Parse(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := redactURL(u); got != want {
			t.Errorf("redactURL(%s)\n got %s\nwant %s", in, got, want)
		}
	}
}

func TestLoggingTransportRedactsRedirect(t *testing.T) {
	var log bytes.Buffer
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/2/archive/resolve" {
			http.Redirect(w, r, "/bucket/data?X-Amz-Signature=s3cr3t&X-Amz-Credential=AKIA", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("file"))
	}))
	defer srv.Close()
	c, err := New(Options{Host: srv.URL, HTTPClient: &http.Client{Transport: LoggingTransport(nil, &log)}})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Do(t.Context(), "GET", srv.URL+"/api/2/archive/resolve?entity=e1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if out := log.String(); strings.Contains(out, "s3cr3t") || strings.Contains(out, "AKIA") || !strings.Contains(out, "X-Amz-Signature=REDACTED") {
		t.Fatalf("log:\n%s", out)
	}
}
