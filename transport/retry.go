// Package transport provides the shared HTTP retry layer every Runpod client
// generated from this repo goes through.
//
// Policy: idempotent requests (GET, HEAD, PUT, DELETE, OPTIONS) retry on 429,
// on 500/502/503/504, and on transport-level errors. Non-idempotent requests
// (POST, PATCH) retry only on 429: a 5xx may mean the API already performed
// the mutation, and a blind replay can double-create. A Retry-After header
// (delay-seconds or HTTP-date, RFC 9110 §10.2.3) is respected up to
// MaxRetryAfter; otherwise delay is exponential backoff with equal jitter.
//
// Two cases opt out of a retry that would otherwise apply. A request whose body
// cannot be rewound (a streamed body, where net/http could not record a
// GetBody) is never replayed, so even a 429 comes straight back to the caller.
// And a delay longer than what is left of the request's deadline is not waited
// out: the response is returned instead, so a caller holding a short timeout
// still sees the 429 and its Retry-After rather than an opaque deadline error.
package transport

import (
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The policy's defaults, used for any Retry field left at its zero value.
const (
	DefaultMaxAttempts   = 4
	DefaultMinBackoff    = 1 * time.Second
	DefaultMaxBackoff    = 30 * time.Second
	DefaultMaxRetryAfter = 60 * time.Second
)

// Retry is an http.RoundTripper that retries per the package policy.
// The zero value is usable: nil Base falls back to http.DefaultTransport and
// zero limits fall back to the package defaults.
type Retry struct {
	Base          http.RoundTripper
	MaxAttempts   int
	MinBackoff    time.Duration
	MaxBackoff    time.Duration
	MaxRetryAfter time.Duration

	// Jitter returns a random float in [0, 1). Overridable for deterministic tests.
	Jitter func() float64
}

func (r *Retry) base() http.RoundTripper {
	if r.Base != nil {
		return r.Base
	}
	return http.DefaultTransport
}

// CloseIdleConnections forwards to the underlying transport, so
// http.Client.CloseIdleConnections is not silently a no-op through this one.
func (r *Retry) CloseIdleConnections() {
	if closer, ok := r.base().(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (r *Retry) maxAttempts() int {
	if r.MaxAttempts > 0 {
		return r.MaxAttempts
	}
	return DefaultMaxAttempts
}

func (r *Retry) RoundTrip(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := 1; ; attempt++ {
		resp, err = r.base().RoundTrip(req)

		if !r.shouldRetry(req, resp, err) || attempt >= r.maxAttempts() {
			return resp, err
		}

		delay := r.delay(attempt, resp)

		// Sleeping past the deadline would throw away a usable response — most
		// costly for a 429, where the status and Retry-After are the only
		// things a caller can act on — and hand back a "while awaiting headers"
		// timeout from a client that was sleeping, not awaiting anything.
		if deadline, ok := req.Context().Deadline(); ok && time.Until(deadline) <= delay {
			return resp, err
		}

		// A Base a caller installed can answer without a body, and a nil one
		// must not panic the SDK.
		if resp != nil && resp.Body != nil {
			// Drain small bodies so their connections can be reused; larger
			// ones are dropped with the connection when closed.
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
		}

		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(delay):
		}

		req, err = rewind(req)
		if err != nil {
			return nil, err
		}
	}
}

// shouldRetry decides whether one more attempt is allowed for this request.
func (r *Retry) shouldRetry(req *http.Request, resp *http.Response, err error) bool {
	if req.Context().Err() != nil {
		return false
	}
	if !replayable(req) {
		return false
	}

	idempotent := false
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		idempotent = true
	}

	if err != nil {
		// Transport-level failure: the request may or may not have reached the
		// server, so only idempotent methods are safe to replay.
		return idempotent && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
	}

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return true // 429 = not processed; safe for any method
	case http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return idempotent
	}
	return false
}

// replayable reports whether the request body can be reconstructed for another attempt.
func replayable(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

// rewind returns a request whose body is reset for the next attempt.
func rewind(req *http.Request) (*http.Request, error) {
	if req.Body == nil || req.Body == http.NoBody || req.GetBody == nil {
		return req, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Body = body
	return clone, nil
}

// delay computes how long to wait before the given (1-based) attempt is retried.
func (r *Retry) delay(attempt int, resp *http.Response) time.Duration {
	maxRetryAfter := r.MaxRetryAfter
	if maxRetryAfter <= 0 {
		maxRetryAfter = DefaultMaxRetryAfter
	}
	if resp != nil {
		if d, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
			return min(d, maxRetryAfter)
		}
	}

	minBackoff := r.MinBackoff
	if minBackoff <= 0 {
		minBackoff = DefaultMinBackoff
	}
	maxBackoff := r.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = DefaultMaxBackoff
	}

	// Cap the exponent so caller-tuned attempt counts cannot overflow the shift.
	shift := min(attempt-1, 20)
	backoff := min(minBackoff<<shift, maxBackoff)

	jitter := r.Jitter
	if jitter == nil {
		jitter = rand.Float64
	}
	// Equal jitter over [backoff/2, backoff].
	return backoff/2 + time.Duration(jitter()*float64(backoff/2))
}

// parseRetryAfter parses a Retry-After header per RFC 9110 §10.2.3: either
// delay-seconds or an HTTP-date.
func parseRetryAfter(header string) (time.Duration, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(header); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(header); err == nil {
		return max(time.Until(t), 0), true
	}
	return 0, false
}
