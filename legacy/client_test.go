package legacy

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/runpod/go-sdk/v2/transport"
)

func testServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// testClient points a client at one httptest server with a plain HTTP client:
// the default retrying transport would sleep seconds before giving up on the
// 5xx cases these tests assert.
func testClient(t *testing.T, url string) *Client {
	t.Helper()
	c, err := New(Config{
		APIKey:     "test-key",
		GraphQLURL: url,
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// captured holds what a handler goroutine saw, for a test goroutine to assert.
// The two run concurrently, so every value goes through the mutex.
type captured struct {
	mu     sync.Mutex
	values map[string]string
}

func newCapture() *captured {
	return &captured{values: map[string]string{}}
}

func (c *captured) set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

func (c *captured) get(key string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.values[key]
}

func TestNewAppliesProductionDefaults(t *testing.T) {
	c, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatal(err)
	}

	if c.GraphQLURL() != DefaultGraphQLURL {
		t.Errorf("graphql url = %q, want %q", c.GraphQLURL(), DefaultGraphQLURL)
	}
	if c.userAgent != DefaultUserAgent {
		t.Errorf("user agent = %q, want %q", c.userAgent, DefaultUserAgent)
	}
	if c.httpClient.Timeout != requestTimeout {
		t.Errorf("timeout = %v, want %v", c.httpClient.Timeout, requestTimeout)
	}
	if _, ok := c.httpClient.Transport.(*transport.Retry); !ok {
		t.Errorf("transport = %T, want the SDK's *transport.Retry", c.httpClient.Transport)
	}
}

func TestNewKeepsConfiguredEndpointsAndClient(t *testing.T) {
	httpClient := &http.Client{}
	c, err := New(Config{
		APIKey:     "test-key",
		GraphQLURL: "https://stage.example.com/graphql",
		UserAgent:  "Terraform/RunPodProvider",
		HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}

	if c.GraphQLURL() != "https://stage.example.com/graphql" {
		t.Errorf("graphql url = %q", c.GraphQLURL())
	}
	if c.userAgent != "Terraform/RunPodProvider" {
		t.Errorf("user agent = %q, want the configured one", c.userAgent)
	}
	if c.httpClient != httpClient {
		t.Error("configured HTTPClient was replaced")
	}
}

func TestNewRequiresAnAPIKey(t *testing.T) {
	// The v2 SDK's runpod.New refuses an empty key rather than deferring to a
	// 401; this constructor matches it.
	if _, err := New(Config{}); err == nil {
		t.Fatal("New without an API key succeeded, want error")
	}
}
