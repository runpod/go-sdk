package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExhaustedFor(t *testing.T) {
	tests := []struct {
		header string
		want   time.Duration
		ok     bool
	}{
		{"", 0, false},
		{`"minute";r=5;t=12, "hour";r=2800;t=1812`, 0, false},
		{`"minute";r=0;t=12, "hour";r=2800;t=1812`, 12 * time.Second, true},
		{`"minute";r=0;t=12, "hour";r=2800;t=1812, "day";r=49500;t=45012`, 12 * time.Second, true}, // the doc example
		{`"minute";r=0;t=12, "hour";r=0;t=1812`, 1812 * time.Second, true},
		{`"minute";t=12;r=0`, 12 * time.Second, true},
		{`"minute";r=0`, 0, false},
		{`"minute";r=x;t=12`, 0, false},
	}
	for _, tt := range tests {
		got, ok := exhaustedFor(tt.header)
		if got != tt.want || ok != tt.ok {
			t.Errorf("exhaustedFor(%q) = %v, %v; want %v, %v", tt.header, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRateLimitPaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("RateLimit", `"minute";r=0;t=1`)
	}))
	t.Cleanup(srv.Close)
	client := &http.Client{Transport: &RateLimit{}}

	get := func(ctx context.Context) time.Duration {
		t.Helper()
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		start := time.Now()
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return time.Since(start)
	}

	get(context.Background()) // reports the minute window exhausted for 1s

	// A deadline shorter than the wait: sent at once.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if took := get(ctx); took > 300*time.Millisecond {
		t.Errorf("request with a short deadline took %v, want it sent without waiting", took)
	}

	// No deadline: held until the window resets.
	if took := get(context.Background()); took < 700*time.Millisecond {
		t.Errorf("request took %v, want it held about 1s for the window to reset", took)
	}

	// A wait above MaxWait: sent at once.
	client.Transport = &RateLimit{MaxWait: 100 * time.Millisecond}
	get(context.Background())
	if took := get(context.Background()); took > 300*time.Millisecond {
		t.Errorf("request took %v, want it sent without waiting past MaxWait", took)
	}
}
