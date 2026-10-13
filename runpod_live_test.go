package runpod

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveCatalogReadOnly exercises the real API with a read-only catalog call.
// Runs only when RUNPOD_API_KEY is set (loaded from .env by the task runner)
// and -short is off, so `task test-offline` stays hermetic with a key present;
// honors RUNPOD_API_BASE_URL for stage.
func TestLiveCatalogReadOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("live smoke test skipped in -short mode")
	}
	if os.Getenv("RUNPOD_API_KEY") == "" {
		t.Skip("RUNPOD_API_KEY not set")
	}

	var opts []Option
	if base := os.Getenv("RUNPOD_API_BASE_URL"); base != "" {
		opts = append(opts, WithBaseURL(base))
	}
	client, err := New(opts...)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	resp, err := client.Catalog.ListGpuTypesWithResponse(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.JSON200 == nil {
		t.Fatalf("status %d: %s", resp.HTTPResponse.StatusCode, resp.Body)
	}
	if len(resp.JSON200.Gpus) == 0 {
		t.Fatal("catalog returned no GPU types")
	}
	t.Logf("catalog returned %d GPU types", len(resp.JSON200.Gpus))
}
