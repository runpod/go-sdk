package transport

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateLimit is an http.RoundTripper that paces requests on the RateLimit
// response header the Runpod API sends. Once a response reports a window
// exhausted, the next request waits for that window to reset. A wait longer
// than MaxWait, or past the request's deadline, is skipped: the request goes
// out and the caller gets the API's 429 and its Retry-After.
//
// A RateLimit paces only the requests that go through it; runpod.New gives each
// Client its own. The zero value is usable: nil Base means
// http.DefaultTransport.
type RateLimit struct {
	Base http.RoundTripper
	// MaxWait is the longest wait RateLimit takes on; 0 means
	// DefaultMaxRetryAfter.
	MaxWait time.Duration

	mu    sync.Mutex
	until time.Time
}

func (l *RateLimit) base() http.RoundTripper {
	if l.Base != nil {
		return l.Base
	}
	return http.DefaultTransport
}

// CloseIdleConnections forwards to the underlying transport.
func (l *RateLimit) CloseIdleConnections() {
	if closer, ok := l.base().(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (l *RateLimit) RoundTrip(req *http.Request) (*http.Response, error) {
	maxWait := l.MaxWait
	if maxWait <= 0 {
		maxWait = DefaultMaxRetryAfter
	}
	l.mu.Lock()
	wait := time.Until(l.until)
	l.mu.Unlock()
	if wait > 0 && wait <= maxWait {
		if deadline, ok := req.Context().Deadline(); !ok || time.Until(deadline) > wait {
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(wait):
			}
		}
	}

	resp, err := l.base().RoundTrip(req)
	if err == nil {
		if reset, ok := exhaustedFor(strings.Join(resp.Header.Values("RateLimit"), ",")); ok {
			l.mu.Lock()
			if until := time.Now().Add(reset); until.After(l.until) {
				l.until = until
			}
			l.mu.Unlock()
		}
	}
	return resp, err
}

// exhaustedFor returns the longest time to reset among the exhausted windows
// (r=0) in a RateLimit header value, and false when no window is exhausted.
// Each comma-separated member is one window: r is the requests left in it and
// t the seconds until it resets. For
//
//	"minute";r=0;t=12, "hour";r=2800;t=1812, "day";r=49500;t=45012
//
// only the minute window is exhausted, so it returns 12s, true.
func exhaustedFor(header string) (time.Duration, bool) {
	var longest time.Duration
	found := false
	for _, member := range strings.Split(header, ",") {
		remaining, reset := -1, -1
		_, params, _ := strings.Cut(member, ";")
		for _, param := range strings.Split(params, ";") {
			key, value, _ := strings.Cut(strings.TrimSpace(param), "=")
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 {
				continue
			}
			switch key {
			case "r":
				remaining = n
			case "t":
				reset = n
			}
		}
		if remaining == 0 && reset > 0 {
			found = true
			longest = max(longest, time.Duration(reset)*time.Second)
		}
	}
	return longest, found
}
