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
	"net/url"
	"os"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/runpod/go-sdk/v2/internal/agent"
	"github.com/runpod/go-sdk/v2/lib/account"
	"github.com/runpod/go-sdk/v2/lib/auditlogs"
	"github.com/runpod/go-sdk/v2/lib/billing"
	"github.com/runpod/go-sdk/v2/lib/catalog"
	"github.com/runpod/go-sdk/v2/lib/clusters"
	"github.com/runpod/go-sdk/v2/lib/costcenters"
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

// Version is this module's release; release.yml reads this line to tag it. Bump
// it in the same commit as the CHANGELOG.md entry.
const Version = "2.2.0"

// Ptr returns a pointer to v, for the optional fields the generated request
// bodies take by pointer.
func Ptr[T any](v T) *T { return &v }

// userAgent identifies the SDK: product and version, OS and architecture, the
// Go release, then the integration tokens and the calling AI agent when there
// are any.
func userAgent(integration string) string {
	ua := "runpod-go-sdk/" + BuildVersion() + " (" + runtime.GOOS + "; " + runtime.GOARCH + ") " + runtime.Version()
	if integration != "" {
		ua += " Integration/" + integration
	}
	if env := agent.Token(os.Getenv("RUNPOD_UA_INTEGRATION")); env != "" && env != integration {
		ua += " Integration/" + env
	}
	if name := agent.Name(); name != "" {
		ua += " (via " + name + ")"
	}
	return ua
}

// Client bundles one authenticated client per API area. Signup is the one tag
// it leaves out: those operations declare no auth, and a caller making them has
// no key yet. Use lib/signup directly.
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
	CostCenters    *costcenters.ClientWithResponses
}

type options struct {
	apiKey      string
	baseURL     string
	userAgent   string
	integration string
	sessionID   string
	httpClient  *http.Client
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

// WithIntegration names the tool built on the SDK, e.g. "terraform", which the
// User-Agent then carries as Integration/<name>. The name may use letters,
// digits, '.', '_' and '-', up to 64 characters. The RUNPOD_UA_INTEGRATION
// environment variable adds a second token when it names something else.
func WithIntegration(name string) Option {
	return func(o *options) { o.integration = name }
}

// WithBaseURL points the client at a non-production API, e.g. stage, and
// overrides the RUNPOD_API_BASE_URL environment variable. The URL names the
// host without the API version: a trailing /v2 is dropped, because every
// operation path starts with /v2, and any other version segment is an error.
func WithBaseURL(url string) Option {
	return func(o *options) { o.baseURL = url }
}

// WithSessionID sends id as the X-Runpod-Session-Id header on every request,
// so the requests of one session or run can be grouped. Off by default.
func WithSessionID(id string) Option {
	return func(o *options) { o.sessionID = id }
}

// WithHTTPClient replaces the default retrying HTTP client entirely.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) { o.httpClient = c }
}

// New builds a Client. The API key comes from WithAPIKey or RUNPOD_API_KEY;
// a missing key is an error rather than a deferred 401.
func New(opts ...Option) (*Client, error) {
	o := options{
		apiKey:  os.Getenv("RUNPOD_API_KEY"),
		baseURL: os.Getenv("RUNPOD_API_BASE_URL"),
	}
	for _, opt := range opts {
		opt(&o)
	}
	if o.baseURL == "" {
		o.baseURL = DefaultBaseURL
	}
	base, err := resolveBaseURL(o.baseURL)
	if err != nil {
		return nil, err
	}
	if o.apiKey == "" {
		return nil, errors.New("runpod: API key required (set RUNPOD_API_KEY or use WithAPIKey)")
	}
	if o.integration != "" && agent.Token(o.integration) != o.integration {
		return nil, fmt.Errorf("runpod: integration %q must be up to 64 of A-Z, a-z, 0-9, '.', '_', '-'", o.integration)
	}
	if o.userAgent == "" {
		o.userAgent = userAgent(o.integration)
	}
	if o.httpClient == nil {
		o.httpClient = &http.Client{
			Timeout:   60 * time.Second,
			Transport: &transport.Retry{},
		}
	}

	headers := func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
		req.Header.Set("User-Agent", o.userAgent)
		req.Header.Set("Accept", accept(req, base.Path))
		if o.sessionID != "" {
			req.Header.Set("X-Runpod-Session-Id", o.sessionID)
		}
		return nil
	}
	o.baseURL = base.String()

	// Every area is wired the same way; the error is wrapped with the area name
	// because a bare one says only that some base URL did not parse.
	c := &Client{}
	if c.Pods, err = pods.NewClientWithResponses(o.baseURL,
		pods.WithHTTPClient(o.httpClient), pods.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: pods client: %w", err)
	}
	if c.Serverless, err = serverless.NewClientWithResponses(o.baseURL,
		serverless.WithHTTPClient(o.httpClient), serverless.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: serverless client: %w", err)
	}
	if c.Templates, err = templates.NewClientWithResponses(o.baseURL,
		templates.WithHTTPClient(o.httpClient), templates.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: templates client: %w", err)
	}
	if c.NetworkVolumes, err = networkvolumes.NewClientWithResponses(o.baseURL,
		networkvolumes.WithHTTPClient(o.httpClient), networkvolumes.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: network volumes client: %w", err)
	}
	if c.GlobalVolumes, err = globalvolumes.NewClientWithResponses(o.baseURL,
		globalvolumes.WithHTTPClient(o.httpClient), globalvolumes.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: global volumes client: %w", err)
	}
	if c.Registries, err = registries.NewClientWithResponses(o.baseURL,
		registries.WithHTTPClient(o.httpClient), registries.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: registries client: %w", err)
	}
	if c.Catalog, err = catalog.NewClientWithResponses(o.baseURL,
		catalog.WithHTTPClient(o.httpClient), catalog.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: catalog client: %w", err)
	}
	if c.Billing, err = billing.NewClientWithResponses(o.baseURL,
		billing.WithHTTPClient(o.httpClient), billing.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: billing client: %w", err)
	}
	if c.Clusters, err = clusters.NewClientWithResponses(o.baseURL,
		clusters.WithHTTPClient(o.httpClient), clusters.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: clusters client: %w", err)
	}
	if c.Account, err = account.NewClientWithResponses(o.baseURL,
		account.WithHTTPClient(o.httpClient), account.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: account client: %w", err)
	}
	if c.Organization, err = organization.NewClientWithResponses(o.baseURL,
		organization.WithHTTPClient(o.httpClient), organization.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: organization client: %w", err)
	}
	if c.AuditLogs, err = auditlogs.NewClientWithResponses(o.baseURL,
		auditlogs.WithHTTPClient(o.httpClient), auditlogs.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: audit logs client: %w", err)
	}
	if c.CostCenters, err = costcenters.NewClientWithResponses(o.baseURL,
		costcenters.WithHTTPClient(o.httpClient), costcenters.WithRequestEditorFn(headers)); err != nil {
		return nil, fmt.Errorf("runpod: cost centers client: %w", err)
	}
	return c, nil
}

// resolveBaseURL checks that raw is an absolute http(s) URL and drops a
// trailing slash and a trailing /v2. Any other trailing version segment, such
// as /v1, is an error: the client only speaks v2.
func resolveBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("runpod: base URL %q must be an absolute http or https URL", raw)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	last := u.Path[strings.LastIndex(u.Path, "/")+1:]
	if last == "v2" {
		u.Path = strings.TrimSuffix(u.Path, "/v2")
	} else if len(last) > 1 && last[0] == 'v' && strings.Trim(last[1:], "0123456789") == "" {
		return nil, fmt.Errorf("runpod: base URL %q ends in API version %s; this client speaks v2 and adds /v2 itself", raw, last)
	}
	u.RawPath = ""
	return u, nil
}

// accept is the Accept header for req: the log-stream operations answer with
// Server-Sent Events, everything else with JSON or, on an error, problem JSON.
func accept(req *http.Request, basePath string) string {
	p := strings.TrimPrefix(req.URL.Path, basePath)
	if req.Method == http.MethodGet {
		if ok, _ := path.Match("/v2/pods/*/logs", p); ok {
			return "text/event-stream"
		}
		if ok, _ := path.Match("/v2/serverless/*/workers/*/logs", p); ok {
			return "text/event-stream"
		}
	}
	return "application/json, application/problem+json"
}
