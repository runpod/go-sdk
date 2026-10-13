# Changelog

This file is maintained by hand, newest release first. See the Releasing
section of [README.md](README.md) for the tagging procedure.

## v2.0.0

First release of the v2 SDK. The module path carries the `/v2` suffix because
it is the Runpod API version; there is no v1 of this module to upgrade from.
The earlier serverless job client, `github.com/runpod/go-sdk/pkg/sdk`, is a
separate module and is unchanged.

### Added

- One generated client package per API area under `lib/`, covering all 60
  operations of the 2026-09-23 production REST v2 spec across nine operation
  tags: account, billing, catalog, clusters, network volumes, pods,
  registries, serverless, templates.
- Account secrets on `client.Account`: `ListSecrets`, `CreateSecret`,
  `GetSecret`, `UpdateSecret`, `DeleteSecret` over `/v2/account/secrets`. A
  secret is an encrypted string a pod, endpoint or template references from an
  environment variable as `{{ RUNPOD_SECRET_<name> }}`, substituted when the
  pod or worker boots. Values are write-only: no read returns one, and the
  response carries `valueLastUpdatedAt` instead.
- Cursor pagination on the five list operations that take `cursor` and `limit`:
  `Pods.ListPods`, `Serverless.ListEndpoints`, `Serverless.ListEndpointBuilds`,
  `Serverless.ListEndpointReleases` and `Templates.ListTemplates`. An omitted
  `limit` is not "everything": it defaults to 1000 (100 for builds), so
  a caller that wants the whole set follows `pagination.nextCursor` until
  `hasNextPage` is false. Every other list operation returns its bare list and
  carries no pagination block.
- `lib/types`, holding every schema, response, parameter and header the spec
  declares. Each area package imports it, so a shape several areas use is one
  Go type.
- `runpod.New` wires all nine onto one `Client` with a bearer key from
  `WithAPIKey` or `RUNPOD_API_KEY`, a base URL (`WithBaseURL`) for stage, and a
  replaceable HTTP client (`WithHTTPClient`).
- A shared retry transport (`transport.Retry`): 429 retries for every method
  honoring `Retry-After`, 5xx and connection errors only for idempotent
  methods, exponential backoff with equal jitter. A request whose body cannot
  be rewound is never replayed, and a delay past the request's deadline returns
  the response instead of a timeout.
- `User-Agent: runpod-go-sdk/<version> (go<release>)` on every request, with
  `WithUserAgent` for callers that need their own attribution.
- `runpod.Ptr` for the optional fields the generated request bodies take by
  pointer.
- A vendored spec (`spec/openapi.yaml`) that is the output of the patch registry
  in `scripts/fix_spec.py`, byte for byte, so `task generate` is reproducible.

### Deprecated

- `github.com/runpod/go-sdk/v2/legacy` is a hand-written client for the REST v1
  host and the GraphQL endpoint, and it is deprecated on arrival. It exists for
  the Terraform provider's compatibility shims, which carry 1.x fields REST v2
  has no route for. Each route is removed as REST v2 grows the same fields, and
  the package goes with the last of them. Nothing else should import it.

### Notes for consumers

- Shared schemas are generated once, into `github.com/runpod/go-sdk/v2/lib/types`,
  and every area package imports them: `types.Pod` is the pod a cluster read
  and a pod read both return, `types.ErrorResponse` is the one error body, and
  the enum constants (`types.CloudSecure`, `types.PodStatusRunning`) live there
  too. An area package declares only its own operations: the client, the
  per-operation parameters, request bodies and response wrappers.
- The published module has one direct dependency,
  `github.com/oapi-codegen/runtime`. The code generator is a separate module
  (`generator/go.mod`), so oapi-codegen and its OpenAPI/YAML tree are not part
  of what this module pulls in.
- This tag freezes the exported surface of every package under `lib/`, of the
  root package, and of `legacy`. Changing any of it needs a `/v3`.
