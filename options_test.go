package runpod

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
	}))
	t.Cleanup(srv.Close)
	client, err := New(WithAPIKey("test-key"), WithBaseURL(srv.URL), WithTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Catalog.ListGpuTypesWithResponse(context.Background(), nil); err == nil {
		t.Error("want a timeout error")
	}
	if _, err := New(WithAPIKey("test-key"), WithTimeout(-time.Second)); err == nil {
		t.Error("WithTimeout(-1s): want an error")
	}
	if _, err := New(WithAPIKey("test-key"), WithTimeout(-time.Second), WithHTTPClient(http.DefaultClient)); err != nil {
		t.Errorf("WithTimeout(-1s) with WithHTTPClient: %v, want it ignored", err)
	}
}

func TestWithTransportKeepsRetry(t *testing.T) {
	var calls atomic.Int32
	rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		status := http.StatusServiceUnavailable
		if calls.Add(1) > 1 {
			status = http.StatusOK
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"0"}}, Body: http.NoBody, Request: r}, nil
	})
	client, err := New(WithAPIKey("test-key"), WithTransport(rt))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Catalog.ListGpuTypes(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || calls.Load() != 2 {
		t.Errorf("status %d after %d calls, want 200 after a retried 503", resp.StatusCode, calls.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMaxRetryAfterBelowTimeout(t *testing.T) {
	for timeout, want := range map[time.Duration]time.Duration{
		0:                      60 * time.Second,
		60 * time.Second:       59 * time.Second,
		5 * time.Minute:        60 * time.Second,
		10 * time.Second:       9 * time.Second,
		2 * time.Second:        time.Second,
		time.Second:            500 * time.Millisecond,
		200 * time.Millisecond: 100 * time.Millisecond,
		time.Nanosecond:        time.Nanosecond,
	} {
		if got := maxRetryAfter(timeout); got != want {
			t.Errorf("maxRetryAfter(%v) = %v, want %v", timeout, got, want)
		}
	}
}
