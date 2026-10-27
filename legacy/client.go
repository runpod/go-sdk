// Package legacy is a hand-written client for the Runpod GraphQL endpoint,
// used only by the Terraform provider's deprecated compatibility shims.
// Nothing in it is generated, and it is not wired onto the root client: v2 API
// work goes through runpod.New and the packages under lib/.
//
// It exists because REST v2 has no route yet for what those shims carry: the
// machine resource and data sources, the account identity, and the handful of
// pod, template and serverless-endpoint fields the 1.x provider carried over.
// Nothing belongs here that is not a 1.x behaviour REST v2 has no field for.
// It sends raw GraphQL documents; the caller owns the query text and the
// shape it reads back.
//
// Deprecated: each use is removed as REST v2 carries the same fields, and the
// package goes with the last of them.
package legacy

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/runpod/go-sdk/v2/transport"
)

const (
	// DefaultGraphQLURL is the production GraphQL endpoint.
	DefaultGraphQLURL = "https://api.runpod.io/graphql"

	// DefaultUserAgent identifies this package in the GraphQL host's access
	// logs, following the v2 SDK's own scheme.
	DefaultUserAgent = "runpod-go-sdk/v2 legacy"

	requestTimeout = 60 * time.Second

	// maxErrorBody caps how much of a failed response is quoted back.
	maxErrorBody = 4096

	// maxResponseBody caps a success body. The shim queries answer with
	// single objects and short lists that sit orders of magnitude below this;
	// the ceiling is there so a broken host cannot stream an unbounded body
	// into a Terraform process's memory.
	maxResponseBody = 32 << 20
)

// Config is the client's input. An empty GraphQLURL falls back to the
// production default; a nil HTTPClient gets the same retrying client shape the
// v2 SDK builds (see transport.Retry: idempotent requests retry on 429 and
// 5xx, POST only on 429).
type Config struct {
	APIKey     string
	GraphQLURL string
	HTTPClient *http.Client

	// UserAgent overrides DefaultUserAgent. A consumer that has its own
	// attribution on the API side passes it here — the Terraform provider
	// sends the value its 1.x releases sent followed by its v2 requests'
	// value, so filters on the 1.x value still match and the shim requests
	// are attributed to the same client as the v2 ones.
	UserAgent string
}

// Client talks to the GraphQL API with a bearer key.
type Client struct {
	apiKey     string
	graphQLURL string
	userAgent  string
	httpClient *http.Client
}

// New builds a Client from cfg. An empty APIKey is an error rather than a
// deferred 401, matching runpod.New in the v2 SDK.
func New(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("legacy: API key required")
	}
	c := &Client{
		apiKey:     cfg.APIKey,
		graphQLURL: cfg.GraphQLURL,
		userAgent:  cfg.UserAgent,
		httpClient: cfg.HTTPClient,
	}
	if c.graphQLURL == "" {
		c.graphQLURL = DefaultGraphQLURL
	}
	if c.userAgent == "" {
		c.userAgent = DefaultUserAgent
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{
			Timeout:   requestTimeout,
			Transport: &transport.Retry{},
		}
	}
	return c, nil
}

// GraphQLURL reports the resolved GraphQL endpoint, for error messages that
// need to say which host was called.
func (c *Client) GraphQLURL() string { return c.graphQLURL }

func (c *Client) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
}

// statusError turns a non-200 response into an error carrying the status and a
// bounded slice of the body, so a stray HTML error page cannot flood the
// Terraform diagnostic. A 404 comes back as *NotFoundError so a caller can
// drop the resource from state instead of failing the read.
func statusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	detail := strings.TrimSpace(string(body))
	if resp.StatusCode == http.StatusNotFound {
		return &NotFoundError{Path: requestPath(resp), Detail: detail}
	}
	return fmt.Errorf("API returned status %d: %s", resp.StatusCode, detail)
}

// requestPath is the route a response came from. http.Client.Do always sets
// Request, but a caller's own transport.Retry.Base need not, and a 404 must not
// panic on the way to the caller.
func requestPath(resp *http.Response) string {
	if resp.Request == nil || resp.Request.URL == nil {
		return "the requested resource"
	}
	return resp.Request.URL.Path
}

// NotFoundError is a 404 from the legacy API.
type NotFoundError struct {
	Path   string
	Detail string
}

func (e *NotFoundError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("%s was not found", e.Path)
	}
	return fmt.Sprintf("%s was not found: %s", e.Path, e.Detail)
}

// IsNotFound reports whether err is, or wraps, a 404.
func IsNotFound(err error) bool {
	var notFound *NotFoundError
	return errors.As(err, &notFound)
}
