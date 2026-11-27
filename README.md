# Runpod Go SDK

Go client for the Runpod v2 REST API, generated from the openly served v2
OpenAPI spec. One package per API area (pods, serverless, templates, networkvolumes,
globalvolumes, registries, catalog, billing, clusters, costcenters, account,
organization, auditlogs) behind a single authenticated
client with retry/backoff built in. Every request and response shape those
areas share is one type in `lib/types`, so a pod read from a cluster is the same
`types.Pod` the pods package returns.

A fourteenth area, `lib/signup`, is not on that client: its operations declare no
auth, so a caller making them has no key to build one with. Use
`signup.NewClientWithResponses` directly.

<!-- runpod-agent-setup:start -->
**For AI agents:** setup instructions for the Runpod skills and MCP server are at https://agents.runpod.io/
<!-- runpod-agent-setup:end -->

## Install

```bash
go get github.com/runpod/go-sdk/v2
```

Requires Go 1.25 or newer. The `/v2` in the module path is the Runpod API
version, and it is also a Go major-version suffix: released versions carry
`v2.x.x` tags. See [CHANGELOG.md](CHANGELOG.md) for what each one changed.

### The earlier SDK in `pkg/sdk`

The earlier client, `github.com/runpod/go-sdk/pkg/sdk`, is a separate module
and is unchanged: existing imports and pinned versions keep working, and
`go get github.com/runpod/go-sdk/pkg/sdk` still resolves to it. It works with the
jobs of one serverless endpoint (run, run sync, status, stream, cancel, health,
purge queue), which the v2 SDK does not cover. The v2 SDK manages resources through the REST v2 API: pods,
endpoints, templates, volumes, registries, billing and the rest. The two
modules can be used side by side.

## Usage

```go
import (
    runpod "github.com/runpod/go-sdk/v2"
    "github.com/runpod/go-sdk/v2/lib/pods"
    "github.com/runpod/go-sdk/v2/lib/types"
)

client, err := runpod.New() // key from RUNPOD_API_KEY
if err != nil { ... }

// The *WithResponse methods return a response wrapper: err covers transport,
// body-read and decode failures. An API-level error arrives as a status code,
// which CheckResponse turns into an *APIError.
gpus, err := client.Catalog.ListGpuTypesWithResponse(ctx, nil)
if err != nil { ... }
if err := runpod.CheckResponse(gpus.HTTPResponse, gpus.Body); err != nil { ... }
for _, gpu := range gpus.JSON200.Gpus { ... }

created, err := client.Pods.CreatePodWithResponse(ctx, pods.CreatePodJSONRequestBody{
    Name:  "training-pod",
    Image: runpod.Ptr("runpod/pytorch:2.8.0-py3.11-cuda12.8.1-cudnn-devel-ubuntu22.04"),
    Gpu:   &types.CreateGpuConfig{Id: "NVIDIA GeForce RTX 4090", Count: runpod.Ptr(1)},
})
if err != nil { ... }
if err := runpod.CheckResponse(created.HTTPResponse, created.Body); err != nil { ... }
pod := created.JSON201 // nil on a 202 (only with queue set): the pod is queued
```

`example_test.go` compiles this block, so a regeneration that moves a request
shape fails the build here rather than in a reader's editor.

An area package holds its operations: the client, the per-operation parameters,
request bodies and response wrappers (`pods.CreatePodJSONRequestBody`,
`pods.CreatePodResult`). Every schema the spec names is in `lib/types`
(`types.Pod`, `types.CreateGpuConfig`, `types.ErrorResponse`, the enum
constants), declared once and imported by all fourteen areas, so
`clusters.ListClusterPodsWithResponse` hands back the same `types.Pod` as
`pods.GetPodWithResponse`, and one error check works across areas.

The generated request bodies take their optional fields by pointer, which is
what `runpod.Ptr` is for: it returns a pointer to any value, so a literal
needs no temporary variable.

Seventeen operations are cursor-paginated: `Pods.ListPods`,
`Pods.ListQueuedPods`, `Serverless.ListEndpoints`,
`Serverless.ListEndpointBuilds`, `Serverless.ListEndpointReleases`,
`Serverless.GetEndpointRequestMetrics`,
`Serverless.GetEndpointColdStartMetrics`,
`Serverless.GetEndpointWorkerMetrics`, `Templates.ListTemplates`,
`Clusters.ListClusters`, `NetworkVolumes.ListNetworkVolumes`,
`GlobalVolumes.ListGlobalVolumes`, `CostCenters.ListCostCenters`,
`CostCenters.ListCostCenterResources`, `Organization.ListOrganizationGroups`,
`Organization.ListOrganizationGroupMembers` and `AuditLogs.ListAuditLogs`. The
three metrics reads page over time buckets, not over a list. Their `limit`
is a page size, not a cap you can leave off: omit it and you get 1000 items, or
100 for the builds list and the two organization group lists, and 200 for the
audit logs. So `ListPodsWithResponse(ctx, nil)` reads like "all
pods" and is one page of them. To see the whole set, follow
`pagination.nextCursor` until `hasNextPage` is false:

```go
var all []types.Pod
params := &pods.ListPodsParams{}
for {
    page, err := client.Pods.ListPodsWithResponse(ctx, params)
    if err != nil { ... }
    if err := runpod.CheckResponse(page.HTTPResponse, page.Body); err != nil { ... }
    all = append(all, page.JSON200.Pods...)
    if !page.JSON200.Pagination.HasNextPage {
        break
    }
    params.Cursor = page.JSON200.Pagination.NextCursor
}
```

`example_test.go` compiles this block too. A cursor is opaque and belongs to
the operation and parameters that issued it; a foreign or malformed one comes
back 422. Every other list operation — the billing reads, the catalog, a
cluster's member pods, registries, ECR delegations, endpoint workers, account
secrets, and the organization's users, invites and roles — returns its whole
result in one response and carries no `pagination` block at all.

A list too large to answer in one response comes back 413, decoded into the
result's `ApplicationproblemJSON413`: lower `limit` on a paginated operation,
or ask for fewer items on one that is not.

`Image` above is a full Runpod PyTorch tag, cuDNN variant and base OS included;
the spec's `runpod/pytorch:2.8.0-py3.11-cuda12.8.1` examples abbreviate the same
image to its framework and CUDA versions.

Options: `WithAPIKey` overrides the environment, `WithBaseURL` targets another
environment (e.g. stage) and overrides `RUNPOD_API_BASE_URL`, `WithIntegration`
adds your tool's name to the `User-Agent`, `WithSessionID` sends
`X-Runpod-Session-Id` on every request (off by default), `WithNoRedirect`
returns a 3xx response as is instead of following it, `WithUserAgent` replaces
the `User-Agent` entirely. `WithTimeout`, `WithTransport` and
`WithHTTPClient` are covered with retries below.

The base URL is the host without the API version: a trailing `/v2` is dropped,
and any other version, such as `/v1`, is an error from `New`. Requests send
`Accept: application/json, application/problem+json`; the two log streams
(`Pods.GetPodLogs`, `Serverless.GetWorkerLogs`) send `Accept: text/event-stream`.

The default `User-Agent` is `runpod-go-sdk/<version> (<os>; <arch>) go<release>`
with `<version>` from `runpod.BuildVersion()`, then `Integration/<name>` from
`WithIntegration` and/or `RUNPOD_UA_INTEGRATION`, then `(via <agent>)` when an
AI coding agent such as Claude Code is detected. The agent names match
runpodctl and runpod-python.

Every request from `runpod.New`'s default client goes through the retry
transport (`transport/`): rate-limited requests (429), 5xx and connection
errors retry only for idempotent methods (GET, HEAD, PUT, DELETE, OPTIONS),
honoring `Retry-After`, with exponential backoff and jitter. A POST or PATCH
is never replayed: a 429 or 5xx does not prove the write was not applied, so
it comes straight back to the caller. Defaults: 4 attempts, 1s to 30s
exponential backoff with equal jitter, `Retry-After` honored up to 60s, or
just under a shorter timeout (59s with the default 60s).

Requests are also paced on the API's `RateLimit` header: once a response
reports a window exhausted, the next request waits for it to reset, unless the
wait is longer than the `Retry-After` limit or would pass the request's
deadline; then it goes out and gets the 429.

`WithTimeout` bounds each call, retries and waits included (default 60s, 0 for
none). It bounds the log streams too, so for a long stream use `WithTimeout(0)`
and a context. `WithTransport` replaces the `http.RoundTripper` underneath (a
proxy, custom TLS) and keeps retry and pacing. `WithHTTPClient` replaces the
whole stack, so none of this applies unless your client sets it up, e.g.
`runpod.WithHTTPClient(&http.Client{Transport: &transport.Retry{MaxAttempts: 6}})`.

Two cases opt out of a retry that would otherwise apply. A request whose body
cannot be rewound (a streamed body, where net/http could not record one) is
never replayed, so even a 429 comes straight back. And a delay longer than what
is left of the request's deadline is not waited out: the response is returned
instead, so a short client timeout still shows you the 429 and its `Retry-After`
rather than an opaque deadline error.

Only clients built with `runpod.New` get any of this: the `User-Agent`,
`Accept` and session headers, the base URL handling, the timeout, retry and
pacing. A generated client built directly, such as
`signup.NewClientWithResponses` or `pods.NewClientWithResponses`, sends plain
requests through whatever HTTP client you give it.

## Repo layout

```
runpod.go              hand-written: options, headers, one client per API area
errors.go, version.go  hand-written: APIError and CheckResponse, BuildVersion
internal/agent/        hand-written: AI-agent detection for the User-Agent
example_test.go        hand-written: the usage example above, compiled
transport/             hand-written: the shared retry and rate-limit RoundTrippers
legacy/                hand-written: deprecated GraphQL client, see below
generator/             hand-written: its own module — splits the spec per tag, runs oapi-codegen
scripts/               hand-written: the spec patch registry, its normalizer and tests
spec/openapi.yaml      vendored: fetched by `task spec-pull`, normalized by the patch layer
lib/<area>/*.gen.go    generated: one package per OpenAPI tag — never edited by hand
lib/types/types.gen.go generated: every schema the areas share — never edited by hand
artifacts/api/         generated intermediates (the shared components spec, one spec
                       per tag, and their codegen configs), gitignored
pkg/, example/         the earlier SDK and the Pulumi provider: separate modules, unchanged
```

Fourteen packages under `lib/` map one-to-one onto the spec's fourteen operation tags,
over one `lib/types` holding the whole `components` object of the spec: schemas,
responses, parameters and headers. The generator emits the components once, as
`artifacts/api/components.yaml`, and gives each per-tag spec a reference to that
file plus an `import-mapping` onto `lib/types`, which is oapi-codegen's shape
for splitting one spec across packages. To change generated output, change the
spec, the patch layer, or `generator/main.go`, then run `task generate`.

`generator/` is a separate Go module. That keeps oapi-codegen and its
OpenAPI/YAML dependency tree out of the published module's requirements, so the
SDK a consumer downloads has one direct dependency
(`github.com/oapi-codegen/runtime`) and their vulnerability scanners never see
a code generator the SDK does not execute at runtime.

## Deprecated legacy client

`github.com/runpod/go-sdk/v2/legacy` is a hand-written client for the GraphQL
endpoint. It is not wired onto `runpod.Client` and takes no part in v2 API
work: its only consumer is the Terraform provider's deprecated compatibility
shims, which carry 1.x fields REST v2 has no route for: the machine resource
and data sources; the provider holds its other GraphQL queries itself. It sends
raw GraphQL documents through `Client.Query`; it has no REST v1 client, because
the REST v1 host retires on 2026-11-15. It has no registry username
read: no supported source reports one. The GraphQL `ContainerRegistryAuth` type
carries no username field and there is no `containerRegistryAuths` root field,
and REST v2 treats the username as write-only.

Each use is removed as REST v2 carries the same fields, and the package goes
with the last of them. Import it only if you are maintaining those shims.

## Regenerating from the spec

The client code under `lib/` is fully generated; never edit it by hand.
Besides the root client and `transport/`, the only hand-written client package
is the deprecated `legacy/`.

Prerequisites: Go 1.25+, [Task](https://taskfile.dev), `curl`, and Python 3
with PyYAML (`pip install pyyaml==6.0.3`, the version ci.yml pins) for the spec patch layer.

```bash
task spec-pull   # fetch the served v2 spec and run the patch layer over it
task spec-test   # check the patch layer and the vendored spec (offline)
task generate    # split per tag and run oapi-codegen -> lib/
task build       # both modules
task vet         # both modules
task test        # both tiers when a key is present in .env
task lint        # golangci-lint over the hand-written packages, both modules
task all         # spec-test + generate + build + vet + test-offline + lint
```

`task all` runs the offline test tier, so the full gate never spends against a
key in `.env`; `task test` is the opt-in that includes the live smoke test.

`task lint` runs golangci-lint v2 (pinned in the Taskfile, config in
`.golangci.yaml`): the standard linter set plus gofmt and goimports, the same
shape Runpod's Go services use. `lib/` carries the generated-code header, so
the linter skips it. Fix a finding there in the spec, the patch layer, or the
generator, never in the output.

`scripts/fix_spec.py` is the single patch point for the vendored spec: live-API
divergences, values that must not ship in a public artifact, and the generated
constant names all register there, never hand-edited into the spec or the
generated code. The layer also normalizes formatting, so `spec/openapi.yaml` is
its output byte for byte and re-running it over the vendored spec changes
nothing; `task spec-test` asserts exactly that. Registered patches today, in the
order they run: placeholder AWS account ids in examples, the post-rebrand `Runpod` casing in
prose (identifiers, URLs and wire values keep theirs), `x-enum-varnames` on
every enum, public-description cleanups (including the two pagination
sentences the schemas contradict), the top-level `Clusters` tag entry the
served spec omits while six operations carry the tag, and a neutral password in
the container-env example. oapi-codegen is version-pinned through the `tool`
directive in `generator/go.mod`.

A patch whose anchor has gone stops the run and names itself, so a fix is never
dropped in silence. Description cleanups are the exception: upstream rewording a
published sentence, or moving the node it sits on, prints one
`stale-description-edit:` line and the run carries on, because nothing is
generated from prose and a stale cleanup must not hold up an SDK regeneration. A wording upstream has since adopted, however it is
wrapped, counts as applied and says nothing.

## Testing

Two tiers, both under `task test`:

- **Offline** (default, no credentials, no network): retry-policy and
  rate-limit pacing tests in `transport/` (backoff, jitter, `Retry-After`,
  which methods may replay, `RateLimit` waits)
  plus client-wiring tests in `runpod_test.go` that drive the generated catalog
  and global volumes clients against an `httptest` server. `task spec-test` covers the patch layer
  separately (Python, no Go toolchain needed).
- **Live smoke** (opt-in): `runpod_live_test.go` runs only when
  `RUNPOD_API_KEY` is set, and issues a single read-only
  `GET /v2/catalog/gpus`. It creates no resources and incurs no charges. Set
  `RUNPOD_API_BASE_URL` to point it at stage.

```bash
task test           # both tiers when a key is present in .env
task test-offline   # offline tier only, even with a key configured
```

## CI

`.github/workflows/ci.yml` runs on every pull request and on pushes to `main`:
golangci-lint on both modules, the unit tests with `-race` on Go 1.25.0 and
current stable, a tidy check, the spec patch-layer tests, a check that
regenerating `lib/` changes nothing, and govulncheck. No API key is configured,
so the live smoke test never runs in CI.

`release.yml` starts after a successful `ci` run on `main` and does the
tagging described under Releasing. It is the only workflow here that writes
anything.

## Releasing

A release is a merge to `main` that moves `Version`; the tag and everything
after it are automatic.

1. Run `task all` and confirm it is clean.
2. Add the release's section to [CHANGELOG.md](CHANGELOG.md): what changed,
   what breaks, and the spec date the generated packages came from.
3. Set `Version` in `runpod.go` to the release, in the same commit as the
   changelog entry. The release workflow reads it.

Once `ci` passes on `main`, `.github/workflows/release.yml` tags the merge
commit, opens the GitHub Release with that changelog section, warms the module
proxy and tells runpod/terraform-provider-runpod, which opens its own bump pull
request. A merge that leaves `Version` on a version already tagged releases
nothing. Only a commit that is on `main` is ever tagged.

A release is three things: the tag, the Release and the dispatch. When one of
them fails the earlier ones stay and every step redoes only what is missing.
Finish it from the failed run with Re-run failed jobs, or from Actions →
release → Run workflow with the release's tag as the ref: both carry on past a
Release that already exists, where the automatic run stops, and that is how a
lost dispatch is sent again too. Run workflow on `main` fits only while main's
head is still the release commit; after anything else merges it finds the tag
on an older commit and does nothing, or, if the tag never got pushed, would
carry the version onto the wrong commit. Pushing a tag that way also needs a
successful `ci` run on that commit, so a release whose `ci` was cancelled is
finished by re-running `ci`, which starts the automatic path again.

The proxy picks the module up from the tag, so `go get
github.com/runpod/go-sdk/v2@v2.0.0` resolves within a few minutes and
pkg.go.dev indexes it shortly after. A tag is immutable once fetched: a
mistake needs a new patch version, never a retag.

## Reference

Full API reference: <https://pkg.go.dev/github.com/runpod/go-sdk/v2>.

## License

[Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0) — see [LICENSE](LICENSE).
