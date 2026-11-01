// Package api is a thin client for the OpenAleph HTTP API (/api/2).
package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"time"
)

// DefaultRetries matches the default of OPENALEPH_MAX_TRIES in the Python client.
const DefaultRetries = 5

// Options configures a Client.
type Options struct {
	Host       string
	APIKey     string
	UserAgent  string
	Retries    int
	HTTPClient *http.Client
	// Sleep replaces time.Sleep for retry backoff, mainly for tests.
	Sleep func(context.Context, time.Duration) error
}

// Client talks to a single OpenAleph instance.
type Client struct {
	base      *url.URL
	apiKey    string
	sessionID string
	userAgent string
	retries   int
	http      *http.Client
	sleep     func(context.Context, time.Duration) error
}

// New creates a Client for the given host (for example https://aleph.example.org).
func New(o Options) (*Client, error) {
	if o.Host == "" {
		return nil, fmt.Errorf("no OpenAleph host configured")
	}
	host, err := url.Parse(o.Host)
	if err != nil || host.Scheme == "" || host.Host == "" {
		return nil, fmt.Errorf("invalid OpenAleph host %q", o.Host)
	}
	base := host.ResolveReference(&url.URL{Path: "/api/2/"})
	c := &Client{
		base:      base,
		apiKey:    o.APIKey,
		sessionID: newSessionID(),
		userAgent: o.UserAgent,
		retries:   o.Retries,
		http:      o.HTTPClient,
		sleep:     o.Sleep,
	}
	if c.userAgent == "" {
		c.userAgent = "oa"
	}
	if c.retries <= 0 {
		c.retries = DefaultRetries
	}
	if c.http == nil {
		c.http = &http.Client{}
	}
	if c.sleep == nil {
		c.sleep = sleepCtx
	}
	return c, nil
}

// Host returns the scheme and host of the instance.
func (c *Client) Host() string { return c.base.Scheme + "://" + c.base.Host }

// Retries returns the maximum number of retries after the first attempt.
func (c *Client) Retries() int { return c.retries }

// URL builds an absolute API URL for path (relative to /api/2/) and query values.
func (c *Client) URL(path string, q url.Values) string {
	u := *c.base
	u.Path = c.base.Path + path
	if len(q) > 0 {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func (c *Client) newRequest(ctx context.Context, method, rawURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("X-Aleph-Session", c.sessionID)
	if c.apiKey != "" && c.sameHost(req.URL) {
		req.Header.Set("Authorization", "ApiKey "+c.apiKey)
	}
	return req, nil
}

// sameHost reports whether u points at the configured instance, so the API
// key is never sent to other hosts (for example in absolute file links).
func (c *Client) sameHost(u *url.URL) bool {
	return u.Scheme == c.base.Scheme && u.Host == c.base.Host
}

// Do sends a request and returns the response for status < 400. The caller
// closes the body. Errors are *Error values.
func (c *Client) Do(ctx context.Context, method, rawURL string, body io.Reader, header http.Header) (*http.Response, error) {
	req, err := c.newRequest(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, newTransportError(ctx, err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, newHTTPError(resp)
	}
	return resp, nil
}

// Request sends an optional JSON body and decodes a JSON response into out
// (if non-nil). It does not retry; wrap it in Retry where the Python client did.
func (c *Client) Request(ctx context.Context, method, rawURL string, in, out any) error {
	var body io.Reader
	header := http.Header{}
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
		header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(ctx, method, rawURL, body, header)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return newTransportError(ctx, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

// Retry runs fn, retrying transient errors with growing random backoff.
// Like the Python client it makes up to Retries()+1 attempts.
func (c *Client) Retry(ctx context.Context, fn func() error) error {
	for attempt := 1; ; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		if !IsTransient(err) || attempt > c.retries {
			return err
		}
		if err := c.sleep(ctx, backoff(attempt)); err != nil {
			return err
		}
	}
}

func backoff(failures int) time.Duration {
	secs := math.Pow(2, float64(max(1, failures))) + mrand.Float64()
	return time.Duration(secs * float64(time.Second))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func newSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
