// Package runpod is the Runpod v2 API client. It wires the per-tag clients
// generated from spec/openapi.yaml (see generator/) behind one authenticated,
// retrying HTTP client.
//
//	client, err := runpod.New() // key from RUNPOD_API_KEY
//	gpus, err := client.Catalog.ListGpuTypesWithResponse(ctx, nil)
package runpod

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/runpod/go-sdk/v2/lib/account"
	"github.com/runpod/go-sdk/v2/lib/auditlogs"
	"github.com/runpod/go-sdk/v2/lib/billing"
	"github.com/runpod/go-sdk/v2/lib/catalog"
	"github.com/runpod/go-sdk/v2/lib/clusters"
	"github.com/runpod/go-sdk/v2/lib/globalvolumes"
	"github.com/runpod/go-sdk/v2/lib/networkvolumes"
	"github.com/runpod/go-sdk/v2/lib/organization"
	"github.com/runpod/go-sdk/v2/lib/pods"
	"github.com/runpod/go-sdk/v2/lib/registries"
	"github.com/runpod/go-sdk/v2/lib/serverless"
	"github.com/runpod/go-sdk/v2/lib/templates"
	"github.com/runpod/go-sdk/v2/transport"
)

// DefaultBaseURL is the production API host; spec paths carry the /v2 prefix.
const DefaultBaseURL = "https://api.runpod.io"

// Version is this module's release, sent in the User-Agent header. Bump it in
// the same commit as the release tag; CHANGELOG.md records what changed.
const Version = "2.1.0"

// Ptr returns a pointer to v, for the optional fields the generated request
// bodies take by pointer.
func Ptr[T any](v T) *T { return &v }

// defaultUserAgent identifies the SDK and the Go release it was built with, so
// Runpod can tell official-SDK traffic from any other Go HTTP client.
func defaultUserAgent() string {
	return "runpod-go-sdk/" + Version + " (" + runtime.Version() + ")"
}

// Client bundles one authenticated client per API area (one per spec tag).
type Client struct {
	Pods           *pods.ClientWithResponses
	Serverless     *serverless.ClientWithResponses
	Templates      *templates.ClientWithResponses
	NetworkVolumes *networkvolumes.ClientWithResponses
	GlobalVolumes  *globalvolumes.ClientWithResponses
	Registries     *registries.ClientWithResponses
	Catalog        *catalog.ClientWithResponses
	Billing        *billing.ClientWithResponses
	Clusters       *clusters.ClientWithResponses
	Account        *account.ClientWithResponses
	Organization   *organization.ClientWithResponses
	AuditLogs      *auditlogs.ClientWithResponses
}

type options struct {
	apiKey     string
	baseURL    string
	userAgent  string
	httpClient *http.Client
}

// Option configures New. The With* functions in this package are its
// implementations.
type Option func(*options)

// WithAPIKey overrides the RUNPOD_API_KEY environment variable.
func WithAPIKey(key string) Option {
	return func(o *options) { o.apiKey = key }
}

// WithUserAgent replaces the SDK's own User-Agent, for a tool that wants its
// requests attributed to itself.
func WithUserAgent(agent string) Option {
	return func(o *options) { o.userAgent = agent }
}

// WithBaseURL points the client at a non-production API, e.g. stage.
func WithBaseURL(url string) Option {
	return func(o *options) { o.baseURL = url }
}

// WithHTTPClient replaces the default retrying HTTP client entirely.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) { o.httpClient = c }
}

// New builds a Client. The API key comes from WithAPIKey or RUNPOD_API_KEY;
// a missing key is an error rather than a deferred 401.
func New(opts ...Option) (*Client, error) {
	o := options{
		apiKey:    os.Getenv("RUNPOD_API_KEY"),
		baseURL:   DefaultBaseURL,
		userAgent: defaultUserAgent(),
	}
	for _, opt := range opts {
		opt(&o)
	}
	if o.apiKey == "" {
		return nil, errors.New("runpod: API key required (set RUNPOD_API_KEY or use WithAPIKey)")
	}
	if o.httpClient == nil {
		o.httpClient = &http.Client{
			Timeout:   60 * time.Second,
			Transport: &transport.Retry{},
		}
	}

	auth := func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
		req.Header.Set("User-Agent", o.userAgent)
		return nil
	}

	// Every area is wired the same way; the error is wrapped with the area name
	// because a bare one says only that some base URL did not parse.
	c := &Client{}
	var err error
	if c.Pods, err = pods.NewClientWithResponses(o.baseURL,
		pods.WithHTTPClient(o.httpClient), pods.WithRequestEditorFn(auth)); err != nil {
		return nil, fmt.Errorf("runpod: pods client: %w", err)
	}
	if c.Serverless, err = serverless.NewClientWithResponses(o.baseURL,
		serverless.WithHTTPClient(o.httpClient), serverless.WithRequestEditorFn(auth)); err != nil {
		return nil, fmt.Errorf("runpod: serverless client: %w", err)
	}
	if c.Templates, err = templates.NewClientWithResponses(o.baseURL,
		templates.WithHTTPClient(o.httpClient), templates.WithRequestEditorFn(auth)); err != nil {
		return nil, fmt.Errorf("runpod: templates client: %w", err)
	}
	if c.NetworkVolumes, err = networkvolumes.NewClientWithResponses(o.baseURL,
		networkvolumes.WithHTTPClient(o.httpClient), networkvolumes.WithRequestEditorFn(auth)); err != nil {
		return nil, fmt.Errorf("runpod: network volumes client: %w", err)
	}
	if c.GlobalVolumes, err = globalvolumes.NewClientWithResponses(o.baseURL,
		globalvolumes.WithHTTPClient(o.httpClient), globalvolumes.WithRequestEditorFn(auth)); err != nil {
		return nil, fmt.Errorf("runpod: global volumes client: %w", err)
	}
	if c.Registries, err = registries.NewClientWithResponses(o.baseURL,
		registries.WithHTTPClient(o.httpClient), registries.WithRequestEditorFn(auth)); err != nil {
		return nil, fmt.Errorf("runpod: registries client: %w", err)
	}
	if c.Catalog, err = catalog.NewClientWithResponses(o.baseURL,
		catalog.WithHTTPClient(o.httpClient), catalog.WithRequestEditorFn(auth)); err != nil {
		return nil, fmt.Errorf("runpod: catalog client: %w", err)
	}
	if c.Billing, err = billing.NewClientWithResponses(o.baseURL,
		billing.WithHTTPClient(o.httpClient), billing.WithRequestEditorFn(auth)); err != nil {
		return nil, fmt.Errorf("runpod: billing client: %w", err)
	}
	if c.Clusters, err = clusters.NewClientWithResponses(o.baseURL,
		clusters.WithHTTPClient(o.httpClient), clusters.WithRequestEditorFn(auth)); err != nil {
		return nil, fmt.Errorf("runpod: clusters client: %w", err)
	}
	if c.Account, err = account.NewClientWithResponses(o.baseURL,
		account.WithHTTPClient(o.httpClient), account.WithRequestEditorFn(auth)); err != nil {
		return nil, fmt.Errorf("runpod: account client: %w", err)
	}
	if c.Organization, err = organization.NewClientWithResponses(o.baseURL,
		organization.WithHTTPClient(o.httpClient), organization.WithRequestEditorFn(auth)); err != nil {
		return nil, fmt.Errorf("runpod: organization client: %w", err)
	}
	if c.AuditLogs, err = auditlogs.NewClientWithResponses(o.baseURL,
		auditlogs.WithHTTPClient(o.httpClient), auditlogs.WithRequestEditorFn(auth)); err != nil {
		return nil, fmt.Errorf("runpod: audit logs client: %w", err)
	}
	return c, nil
}
