package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastRetry returns a Retry tuned so tests complete in milliseconds.
func fastRetry() *Retry {
	return &Retry{
		MaxAttempts: 4,
		MinBackoff:  time.Millisecond,
		MaxBackoff:  4 * time.Millisecond,
		Jitter:      func() float64 { return 0 },
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// failingBase counts attempts and always fails at the transport level, which is
// the one branch a live server cannot produce on demand.
func failingBase(attempts *atomic.Int64) roundTripperFunc {
	return func(*http.Request) (*http.Response, error) {
		attempts.Add(1)
		return nil, io.ErrUnexpectedEOF
	}
}

func countingServer(t *testing.T, handler func(n int64, w http.ResponseWriter)) (*httptest.Server, *int64) {
	t.Helper()
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handler(atomic.AddInt64(&calls, 1), w)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestGetRetriesServerErrorsUntilSuccess(t *testing.T) {
	srv, calls := countingServer(t, func(n int64, w http.ResponseWriter) {
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	client := &http.Client{Transport: fastRetry()}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt64(calls); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestGetGivesUpAfterMaxAttempts(t *testing.T) {
	srv, calls := countingServer(t, func(_ int64, w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadGateway)
	})

	client := &http.Client{Transport: fastRetry()}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	if got := atomic.LoadInt64(calls); got != 4 {
		t.Fatalf("attempts = %d, want 4 (MaxAttempts)", got)
	}
}

func TestPostDoesNotRetryServerError(t *testing.T) {
	srv, calls := countingServer(t, func(_ int64, w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	client := &http.Client{Transport: fastRetry()}
	resp, err := client.Post(srv.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Fatalf("attempts = %d, want 1 (5xx POST must not replay)", got)
	}
}

func TestPostRetries429WithReplayedBody(t *testing.T) {
	var lastBody atomic.Value
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		lastBody.Store(string(buf[:n]))
		if atomic.AddInt64(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: fastRetry()}
	resp, err := client.Post(srv.URL, "application/json", strings.NewReader(`{"name":"pod"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt64(&calls); got != 2 {
		t.Fatalf("attempts = %d, want 2 (429 retries POST)", got)
	}
	if got := lastBody.Load(); got != `{"name":"pod"}` {
		t.Fatalf("replayed body = %q, want original body", got)
	}
}

func TestContextCancellationStopsRetries(t *testing.T) {
	srv, _ := countingServer(t, func(_ int64, w http.ResponseWriter) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	retry := fastRetry()
	// Force the wait branch: both bounds long so the capped delay stays long.
	retry.MinBackoff = time.Hour
	retry.MaxBackoff = time.Hour
	client := &http.Client{Transport: retry}

	// A cancellation rather than a deadline: a context with a deadline shorter
	// than the delay never reaches the wait; it gets the response back
	// (TestRetryAfterPastDeadlineReturns429), so an explicit
	// cancel is what this branch has to interrupt.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer time.AfterFunc(20*time.Millisecond, cancel).Stop()

	_, err = client.Do(req)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestGetTransportErrorIsRetried(t *testing.T) {
	var attempts atomic.Int64
	client := &http.Client{Transport: &Retry{
		MinBackoff: time.Millisecond,
		MaxBackoff: time.Millisecond,
		Jitter:     func() float64 { return 0 },
		Base:       failingBase(&attempts),
	}}

	if _, err := client.Get("http://example.invalid/x"); err == nil {
		t.Fatal("a transport failure on every attempt must surface as an error")
	}
	if got := attempts.Load(); got != DefaultMaxAttempts {
		t.Fatalf("attempts = %d, want %d (DefaultMaxAttempts)", got, DefaultMaxAttempts)
	}
}

func TestPostTransportErrorIsNotRetried(t *testing.T) {
	// The branch that decides whether a failed mutation is replayed: a
	// transport error may mean the request reached the server anyway.
	var attempts atomic.Int64
	client := &http.Client{Transport: &Retry{
		MinBackoff: time.Millisecond,
		Jitter:     func() float64 { return 0 },
		Base:       failingBase(&attempts),
	}}

	if _, err := client.Post("http://example.invalid/x", "application/json", strings.NewReader(`{}`)); err == nil {
		t.Fatal("a transport failure must surface as an error")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1 (a POST is never replayed after a transport error)", got)
	}
}

func TestPost429WithAStreamedBodyIsNotReplayed(t *testing.T) {
	srv, calls := countingServer(t, func(_ int64, w http.ResponseWriter) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	// io.NopCloser hides the concrete reader, so http.NewRequest cannot record
	// a GetBody and the body is unrewindable.
	body := io.NopCloser(strings.NewReader(`{"name":"pod"}`))
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL, body)
	if err != nil {
		t.Fatal(err)
	}
	if req.GetBody != nil {
		t.Fatal("the fixture is wrong: this body is rewindable")
	}

	client := &http.Client{Transport: fastRetry()}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 handed back", resp.StatusCode)
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Fatalf("attempts = %d, want 1 (a streamed body cannot be replayed)", got)
	}
}

func TestRetryAfterPastDeadlineReturns429(t *testing.T) {
	srv, calls := countingServer(t, func(_ int64, w http.ResponseWriter) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	client := &http.Client{Timeout: 300 * time.Millisecond, Transport: &Retry{}}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("err = %v, want the 429 rather than a timeout", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-After"); got != "30" {
		t.Fatalf("Retry-After = %q, want the header the caller has to schedule against", got)
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestAResponseWithNoBodyDoesNotPanic(t *testing.T) {
	// Base is exported, so a caller's instrumenting or mocking transport can
	// answer a retryable status without a body.
	var attempts atomic.Int64
	client := &http.Client{Transport: &Retry{
		MaxAttempts: 2,
		MinBackoff:  time.Millisecond,
		Jitter:      func() float64 { return 0 },
		Base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{}}, nil
		}),
	}}

	resp, err := client.Get("http://example.invalid/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2 (MaxAttempts)", got)
	}
}

func TestCloseIdleConnectionsReachesTheBase(t *testing.T) {
	base := &closeCountingTransport{}
	(&Retry{Base: base}).CloseIdleConnections()
	if base.closed != 1 {
		t.Fatalf("base CloseIdleConnections calls = %d, want 1", base.closed)
	}
}

type closeCountingTransport struct{ closed int }

func (t *closeCountingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("not used")
}

func (t *closeCountingTransport) CloseIdleConnections() { t.closed++ }

func TestParseRetryAfter(t *testing.T) {
	if d, ok := parseRetryAfter("7"); !ok || d != 7*time.Second {
		t.Fatalf("delay-seconds: got (%v, %v), want (7s, true)", d, ok)
	}
	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if d, ok := parseRetryAfter(future); !ok || d < 85*time.Second || d > 90*time.Second {
		t.Fatalf("http-date: got (%v, %v), want ~90s", d, ok)
	}
	past := time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat)
	if d, ok := parseRetryAfter(past); !ok || d != 0 {
		t.Fatalf("past http-date: got (%v, %v), want (0, true)", d, ok)
	}
	if _, ok := parseRetryAfter("garbage"); ok {
		t.Fatal("garbage header parsed as valid")
	}
	if _, ok := parseRetryAfter(""); ok {
		t.Fatal("empty header parsed as valid")
	}
}

func TestRetryAfterCapped(t *testing.T) {
	r := fastRetry()
	r.MaxRetryAfter = 2 * time.Millisecond
	resp := &http.Response{Header: http.Header{"Retry-After": []string{"3600"}}}
	if d := r.delay(1, resp); d != 2*time.Millisecond {
		t.Fatalf("delay = %v, want capped 2ms", d)
	}
}
