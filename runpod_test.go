package runpod

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/runpod/go-sdk/v2/internal/agent"
	"github.com/runpod/go-sdk/v2/lib/globalvolumes"
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
	// Another area is a new field on Client and a new block in New; a forgotten
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
		// lib/types holds the schemas every area package shares, and lib/signup's
		// operations declare no auth, so a caller of them has no key to build a
		// Client with. Neither carries a field.
		if entry.IsDir() && entry.Name() != "types" && entry.Name() != "signup" {
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

func TestUserAgentFormat(t *testing.T) {
	base := "runpod-go-sdk/" + BuildVersion() + " (" + runtime.GOOS + "; " + runtime.GOARCH + ") " + runtime.Version()
	tests := []struct {
		name        string
		integration string
		env         map[string]string
		want        string
	}{
		{"plain", "", nil, base},
		{"integration", "terraform", nil, base + " Integration/terraform"},
		{"env integration", "", map[string]string{"RUNPOD_UA_INTEGRATION": "my-tool"}, base + " Integration/my-tool"},
		{"both", "terraform", map[string]string{"RUNPOD_UA_INTEGRATION": "my-tool"}, base + " Integration/terraform Integration/my-tool"},
		{"same name once", "terraform", map[string]string{"RUNPOD_UA_INTEGRATION": "terraform"}, base + " Integration/terraform"},
		{"env integration cleaned", "", map[string]string{"RUNPOD_UA_INTEGRATION": "my tool\r\nX: 1"}, base + " Integration/mytoolX1"},
		{"agent", "terraform", map[string]string{"CLAUDECODE": "1"}, base + " Integration/terraform (via claude-code)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, v := range append(agent.EnvVars(), "RUNPOD_UA_INTEGRATION") {
				t.Setenv(v, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if got := userAgent(tt.integration); got != tt.want {
				t.Errorf("userAgent(%q) = %q, want %q", tt.integration, got, tt.want)
			}
		})
	}
}

func TestNewRejectsInvalidIntegration(t *testing.T) {
	for _, name := range []string{"my tool", "tool/1.0", strings.Repeat("a", 65)} {
		if _, err := New(WithAPIKey("test-key"), WithIntegration(name)); err == nil {
			t.Errorf("WithIntegration(%q): want an error", name)
		}
	}
}

func TestResolveBaseURL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://api.runpod.io", "https://api.runpod.io"},
		{"https://api.runpod.io/", "https://api.runpod.io"},
		{"https://api.runpod.io/v2", "https://api.runpod.io"},
		{"https://api.runpod.io/v2/", "https://api.runpod.io"},
		{"https://proxy.example.com/runpod/v2", "https://proxy.example.com/runpod"},
		{"https://api.runpod.io/v1", ""},
		{"https://api.runpod.io/v3/", ""},
		{"api.runpod.io", ""},
		{"ftp://api.runpod.io", ""},
	}
	for _, tt := range tests {
		got, err := resolveBaseURL(tt.in)
		switch {
		case tt.want == "" && err == nil:
			t.Errorf("resolveBaseURL(%q) = %q, want an error", tt.in, got)
		case tt.want != "" && err != nil:
			t.Errorf("resolveBaseURL(%q): %v", tt.in, err)
		case tt.want != "" && got.String() != tt.want:
			t.Errorf("resolveBaseURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBaseURLOptionOverEnvironment(t *testing.T) {
	var gotPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(gpuTypesJSON))
	}))
	t.Cleanup(srv.Close)

	// The environment alone is used, with its /v2 dropped.
	t.Setenv("RUNPOD_API_BASE_URL", srv.URL+"/v2/")
	client, err := New(WithAPIKey("test-key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Catalog.ListGpuTypesWithResponse(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := gotPath.Load(); got != "/v2/catalog/gpus" {
		t.Errorf("path = %q, want /v2/catalog/gpus", got)
	}

	// The option wins over an environment value that would fail.
	t.Setenv("RUNPOD_API_BASE_URL", "https://api.runpod.io/v1")
	if _, err := New(WithAPIKey("test-key")); err == nil {
		t.Error("New with RUNPOD_API_BASE_URL ending in /v1: want an error")
	}
	if _, err := New(WithAPIKey("test-key"), WithBaseURL(srv.URL)); err != nil {
		t.Errorf("New with WithBaseURL over a bad environment value: %v", err)
	}
}

func TestRequestHeaders(t *testing.T) {
	var got atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Header.Clone())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(gpuTypesJSON))
	}))
	t.Cleanup(srv.Close)
	ctx := context.Background()
	header := func() http.Header { h, _ := got.Load().(http.Header); return h }

	client, err := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Catalog.ListGpuTypesWithResponse(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if a := header().Get("Accept"); a != "application/json, application/problem+json" {
		t.Errorf("Accept = %q, want JSON and problem JSON", a)
	}
	if _, ok := header()["X-Runpod-Session-Id"]; ok {
		t.Error("X-Runpod-Session-Id sent without WithSessionID")
	}

	client, err = New(WithAPIKey("test-key"), WithBaseURL(srv.URL), WithSessionID("session-1"))
	if err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() (*http.Response, error){
		"pod logs":    func() (*http.Response, error) { return client.Pods.GetPodLogs(ctx, "pod-1", nil) },
		"worker logs": func() (*http.Response, error) { return client.Serverless.GetWorkerLogs(ctx, "ep-1", "worker-1", nil) },
	} {
		resp, err := call()
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if a := header().Get("Accept"); a != "text/event-stream" {
			t.Errorf("%s: Accept = %q, want text/event-stream", name, a)
		}
		if s := header().Get("X-Runpod-Session-Id"); s != "session-1" {
			t.Errorf("%s: X-Runpod-Session-Id = %q, want session-1", name, s)
		}
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

func TestGlobalVolumesCreate(t *testing.T) {
	// Global volumes are the first area whose create takes only a name, so the
	// request body is asserted whole: a regeneration that grew a required field
	// would change it.
	var gotMethod, gotPath, gotBody atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotMethod.Store(r.Method)
		gotPath.Store(r.URL.Path)
		gotBody.Store(string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"gsb_abc","name":"training-data","createdAt":"2026-09-24T00:00:00Z","createdByUserId":null}`))
	}))
	t.Cleanup(srv.Close)

	client, err := New(WithAPIKey("test-key"), WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.GlobalVolumes.CreateGlobalVolumeWithResponse(context.Background(),
		globalvolumes.CreateGlobalVolumeJSONRequestBody{Name: "training-data"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.JSON201 == nil {
		t.Fatalf("JSON201 = nil, status %d, body %s", resp.HTTPResponse.StatusCode, resp.Body)
	}
	if got := gotMethod.Load(); got != http.MethodPost {
		t.Errorf("method = %q, want POST", got)
	}
	if got := gotPath.Load(); got != "/v2/global-volumes" {
		t.Errorf("path = %q, want /v2/global-volumes", got)
	}
	if got := gotBody.Load(); got != `{"name":"training-data"}` {
		t.Errorf("body = %s, want {\"name\":\"training-data\"}", got)
	}
	if resp.JSON201.Id != "gsb_abc" || resp.JSON201.CreatedByUserId != nil {
		t.Errorf("unexpected decode: %+v", resp.JSON201)
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
