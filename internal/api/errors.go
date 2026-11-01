package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
)

// Error is returned for failed API calls.
type Error struct {
	// Status is the HTTP status code, 0 for transport errors.
	Status  int
	Message string
	// Transient errors (connection problems, timeouts, HTTP >= 500) may be retried.
	Transient bool
	Err       error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Err }

// IsTransient reports whether err is an *Error worth retrying.
func IsTransient(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Transient
}

// StatusCode returns the HTTP status of err, or 0.
func StatusCode(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}

func newTransportError(ctx context.Context, err error) *Error {
	e := &Error{Message: err.Error(), Err: err}
	if ctx.Err() != nil {
		return e
	}
	var ne net.Error
	e.Transient = errors.As(err, &ne) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
	return e
}

func newHTTPError(resp *http.Response) *Error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	msg := strings.TrimSpace(string(body))
	var data struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &data) == nil && data.Message != "" {
		msg = data.Message
	}
	if msg == "" {
		msg = resp.Status
	}
	return &Error{Status: resp.StatusCode, Message: msg, Transient: resp.StatusCode >= 500}
}
