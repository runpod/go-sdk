// Package legacy is a hand-written client for the Runpod REST v1 host and the
// GraphQL endpoint, used only by the Terraform provider's deprecated
// compatibility shims. Nothing in it is generated, and it is not wired onto
// the root client: v2 API work goes through runpod.New and the packages under
// lib/.
//
// It exists because REST v2 has no route yet for what those shims carry. On
// GraphQL: the machine resource and data sources, and the registry usernames.
// On REST v1: the account user read, the three billing reads, and the pod,
// template and serverless-endpoint fields the 1.x provider carried over.
// Nothing belongs here that is not a 1.x behaviour REST v2 has no field for.
//
// Every field name in the typed routes is the v1 wire name observed from the
// 1.x provider's REST v1 requests (runpod/terraform-provider-runpod v1.0.8),
// and each struct holds only the fields a 2.0.0 shim needs rather than the
// whole v1 object: everything else comes from the v2 SDK.
//
// Deprecated: each route here is removed as REST v2 carries the same fields,
// and the package goes with the last of them.
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
	// DefaultRestV1URL is the production REST v1 base URL; paths are appended to it.
	DefaultRestV1URL = "https://rest.runpod.io/v1"

	// DefaultUserAgent identifies this package in the legacy hosts' access
	// logs, following the v2 SDK's own scheme.
	DefaultUserAgent = "runpod-go-sdk/v2 legacy"

	requestTimeout = 60 * time.Second

	// maxErrorBody caps how much of a failed response is quoted back.
	maxErrorBody = 4096

	// maxResponseBody caps a success body. The legacy routes answer with
	// configuration objects and billing arrays that sit orders of magnitude
	// below this; the ceiling is there so a broken host cannot stream an
	// unbounded body into a Terraform process's memory.
	maxResponseBody = 32 << 20
)

// Config is the client's input. Empty URLs fall back to the production
// defaults; a nil HTTPClient gets the same retrying client shape the v2 SDK
// builds (see transport.Retry: idempotent requests retry on 429 and 5xx,
// POST only on 429).
type Config struct {
	APIKey     string
	GraphQLURL string
	RestV1URL  string
	HTTPClient *http.Client

	// UserAgent overrides DefaultUserAgent. A consumer that has its own
	// attribution on the API side passes it here — the Terraform provider
	// sends the value its 1.x releases sent followed by its v2 requests'
	// value, so filters on the 1.x value still match and the shim requests
	// are attributed to the same client as the v2 ones.
	UserAgent string
}

// Client talks to the legacy GraphQL and REST v1 APIs with a bearer key.
type Client struct {
	apiKey     string
	graphQLURL string
	restV1URL  string
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
		restV1URL:  cfg.RestV1URL,
		userAgent:  cfg.UserAgent,
		httpClient: cfg.HTTPClient,
	}
	if c.graphQLURL == "" {
		c.graphQLURL = DefaultGraphQLURL
	}
	if c.restV1URL == "" {
		c.restV1URL = DefaultRestV1URL
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

// RestV1URL reports the resolved REST v1 base URL, for error messages that
// need to say which host was called.
func (c *Client) RestV1URL() string { return c.restV1URL }

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

// DecodeError is a response the host answered successfully and this client
// could not read: a body that is not the expected JSON shape. It is a distinct
// type because on a write it says the opposite of a failure — the host accepted
// the change and only the read-back is unusable, so the caller can report it as
// a warning with null read-backs instead of failing an apply that landed.
// v1.0.8 read every one of these fields with a checked type assertion and
// skipped what did not match rather than failing the call.
type DecodeError struct {
	Err error
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("decoding the REST v1 response failed: %v", e.Err)
}

func (e *DecodeError) Unwrap() error { return e.Err }

// IsDecodeFailure reports whether err is, or wraps, an unreadable response body.
func IsDecodeFailure(err error) bool {
	var decode *DecodeError
	return errors.As(err, &decode)
}
