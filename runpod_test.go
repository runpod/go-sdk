package runpod

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runpod/go-sdk/v2/transport"
)

const gpuTypesJSON = `{"gpus":[{
	"id": "NVIDIA GeForce RTX 4090",
	"name": "RTX 4090",
	"pool": "ADA_24",
	"manufacturer": "NVIDIA",
	"memory": 24,
	"secure": true,
	"community": true,
	"price": {"secure": 0.44, "community": 0.31},
	"maxCount": {"secure": 8, "community": 4}
}]}`

func fastHTTPClient() *http.Client {
	return &http.Client{
		Transport: &transport.Retry{
			MinBackoff: time.Millisecond,
			MaxBackoff: 4 * time.Millisecond,
		},
	}
}

func TestNewRequiresAPIKey(t *testing.T) {
	t.Setenv("RUNPOD_API_KEY", "")
	if _, err := New(); err == nil {
		t.Fatal("New() without a key succeeded, want error")
	}
}

func TestEveryAreaClientIsWired(t *testing.T) {
	// A tenth area is a new field on Client and a new block in New; a forgotten
	// one compiles, lints clean and is unreachable from the SDK. The lib/ count
	// is what catches a generated package nobody wired.
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}

	value := reflect.ValueOf(*client)
	for i := range value.NumField() {
		if value.Field(i).IsNil() {
			t.Errorf("Client.%s is nil after New", value.Type().Field(i).Name)
		}
	}

	entries, err := os.ReadDir("lib")
	if err != nil {
		t.Fatal(err)
	}
	areas := 0
	for _, entry := range entries {
		// lib/types holds the schemas every area package shares; it carries no
		// client and so no field on Client.
		if entry.IsDir() && entry.Name() != "types" {
			areas++
		}
	}
	if areas != value.NumField() {
		t.Errorf("lib/ holds %d generated area packages, Client has %d fields", areas, value.NumField())
	}
}

func TestUserAgentIdentifiesTheSDK(t *testing.T) {
	var gotAgent atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAgent.Store(r.Header.Get("User-Agent"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(gpuTypesJSON))
	}))
	t.Cleanup(srv.Close)

	client, err := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Catalog.ListGpuTypesWithResponse(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	agent, _ := gotAgent.Load().(string)
	if !strings.HasPrefix(agent, "runpod-go-sdk/"+Version) {
		t.Errorf("User-Agent = %q, want the SDK and its version", agent)
	}

	override, err := New(WithAPIKey("test-key"), WithBaseURL(srv.URL), WithUserAgent("my-tool/1.0"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := override.Catalog.ListGpuTypesWithResponse(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := gotAgent.Load(); got != "my-tool/1.0" {
		t.Errorf("User-Agent = %q, want the caller's override", got)
	}
}

func TestCatalogListGpuTypes(t *testing.T) {
	var gotPath, gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		gotAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(gpuTypesJSON))
	}))
	t.Cleanup(srv.Close)

	client, err := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Catalog.ListGpuTypesWithResponse(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.JSON200 == nil {
		t.Fatalf("JSON200 = nil, status %d, body %s", resp.HTTPResponse.StatusCode, resp.Body)
	}
	if got := gotPath.Load(); got != "/v2/catalog/gpus" {
		t.Fatalf("path = %q, want /v2/catalog/gpus", got)
	}
	if got := gotAuth.Load(); got != "Bearer test-key" {
		t.Fatalf("Authorization = %q, want Bearer test-key", got)
	}
	gpus := resp.JSON200.Gpus
	if len(gpus) != 1 || gpus[0].Id != "NVIDIA GeForce RTX 4090" || gpus[0].Memory != 24 {
		t.Fatalf("unexpected decode: %+v", gpus)
	}
}

func TestRetryWiredThroughGeneratedClient(t *testing.T) {
	var calls int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt64(&calls, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(gpuTypesJSON))
	}))
	t.Cleanup(srv.Close)

	client, err := New(WithAPIKey("test-key"), WithBaseURL(srv.URL), WithHTTPClient(fastHTTPClient()))
	if err != nil {
		t.Fatal(err)
	}

	resp, err := client.Catalog.ListGpuTypesWithResponse(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.JSON200 == nil {
		t.Fatalf("JSON200 = nil after retry, status %d", resp.HTTPResponse.StatusCode)
	}
	if got := atomic.LoadInt64(&calls); got != 2 {
		t.Fatalf("server calls = %d, want 2 (one 503 + one retry)", got)
	}
}
