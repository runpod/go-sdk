# Runpod Go SDK

Go client for the Runpod v2 REST API, generated from the openly served v2
OpenAPI spec. One package per API area (pods, serverless, templates, networkvolumes,
globalvolumes, registries, catalog, billing, clusters, account, organization,
auditlogs) behind a single authenticated
client with retry/backoff built in. Every request and response shape those
areas share is one type in `lib/types`, so a pod read from a cluster is the same
`types.Pod` the pods package returns.

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
// so check the typed status field for it.
gpus, err := client.Catalog.ListGpuTypesWithResponse(ctx, nil)
if err != nil { ... }
if gpus.JSON200 == nil {
    log.Fatalf("catalog: HTTP %d: %s", gpus.HTTPResponse.StatusCode, gpus.Body)
}
for _, gpu := range gpus.JSON200.Gpus { ... }

created, err := client.Pods.CreatePodWithResponse(ctx, pods.CreatePodJSONRequestBody{
    Name:  "training-pod",
    Image: runpod.Ptr("runpod/pytorch:2.8.0-py3.11-cuda12.8.1-cudnn-devel-ubuntu22.04"),
    Gpu:   &types.CreateGpuConfig{Id: "NVIDIA GeForce RTX 4090", Count: runpod.Ptr(1)},
})
if err != nil { ... }
if created.JSON201 == nil {
    log.Fatalf("create pod: HTTP %d: %s", created.HTTPResponse.StatusCode, created.Body)
}
pod := created.JSON201
```

`example_test.go` compiles this block, so a regeneration that moves a request
shape fails the build here rather than in a reader's editor.

An area package holds its operations: the client, the per-operation parameters,
request bodies and response wrappers (`pods.CreatePodJSONRequestBody`,
`pods.CreatePodResult`). Every schema the spec names is in `lib/types`
(`types.Pod`, `types.CreateGpuConfig`, `types.ErrorResponse`, the enum
constants), declared once and imported by all twelve areas, so
`clusters.ListClusterPodsWithResponse` hands back the same `types.Pod` as
`pods.GetPodWithResponse`, and one error check works across areas.

The generated request bodies take their optional fields by pointer, which is
what `runpod.Ptr` is for: it returns a pointer to any value, so a literal
needs no temporary variable.

Nine list operations are cursor-paginated: `Pods.ListPods`,
`Serverless.ListEndpoints`, `Serverless.ListEndpointBuilds`,
`Serverless.ListEndpointReleases`, `Templates.ListTemplates`,
`Clusters.ListClusters`, `NetworkVolumes.ListNetworkVolumes`,
`GlobalVolumes.ListGlobalVolumes` and `AuditLogs.ListAuditLogs`. Their `limit`
is a page size, not a cap you can leave off: omit it and you get 1000 items, or
100 for the builds list and 200 for the audit logs. So `ListPodsWithResponse(ctx, nil)` reads like "all
pods" and is one page of them. To see the whole set, follow
`pagination.nextCursor` until `hasNextPage` is false:

```go
var all []types.Pod
params := &pods.ListPodsParams{}
for {
    page, err := client.Pods.ListPodsWithResponse(ctx, params)
    if err != nil { ... }
    if page.JSON200 == nil {
        log.Fatalf("list pods: HTTP %d: %s", page.HTTPResponse.StatusCode, page.Body)
    }
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
cluster's member pods, registries, ECR delegations, endpoint workers, and
account secrets — returns its whole result in one response and carries no
`pagination` block at all.

A list too large to answer in one response comes back 413, decoded into the
result's `ApplicationproblemJSON413`: lower `limit` on a paginated operation,
or ask for fewer items on one that is not.

`Image` above is a full Runpod PyTorch tag, cuDNN variant and base OS included;
the spec's `runpod/pytorch:2.8.0-py3.11-cuda12.8.1` examples abbreviate the same
image to its framework and CUDA versions.

Options: `WithAPIKey` overrides the environment, `WithBaseURL` targets another
environment (e.g. stage), `WithUserAgent` replaces the SDK's own `User-Agent`
(`runpod-go-sdk/<version> (go<release>)`) for a tool that wants its requests
attributed to itself, `WithHTTPClient` replaces the HTTP client entirely.

Every request goes through the shared retry transport (`transport/`):
rate-limited requests (429) retry for all methods honoring `Retry-After`;
5xx and connection errors retry only for idempotent methods, with exponential
backoff and jitter. A failed POST is never blindly replayed. Defaults: 4
attempts, 1s to 30s exponential backoff with equal jitter, `Retry-After`
honored up to 60s, inside a 60s client timeout. To tune it, pass your own
client: `runpod.WithHTTPClient(&http.Client{Transport: &transport.Retry{MaxAttempts: 6}})`.

Two cases opt out of a retry that would otherwise apply. A request whose body
cannot be rewound (a streamed body, where net/http could not record one) is
never replayed, so even a 429 comes straight back. And a delay longer than what
is left of the request's deadline is not waited out: the response is returned
instead, so a short client timeout still shows you the 429 and its `Retry-After`
rather than an opaque deadline error.

## Repo layout

```
runpod.go              hand-written: options, auth, one client per API area
example_test.go        hand-written: the usage example above, compiled
transport/             hand-written: the shared retry RoundTripper
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

Twelve packages under `lib/` map one-to-one onto the spec's twelve operation tags,
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

## Testing

Two tiers, both under `task test`:

- **Offline** (default, no credentials, no network): retry-policy tests in
  `transport/` (backoff, jitter, `Retry-After`, which methods may replay)
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

## Releasing

Releases are tagged by hand; there is no release automation.

1. Run `task all` and confirm it is clean.
2. Add the release's section to [CHANGELOG.md](CHANGELOG.md): what changed,
   what breaks, and the spec date the generated packages came from.
3. Set `Version` in `runpod.go` to the release, in the same commit as the
   changelog entry. It is the value the `User-Agent` header carries.
4. Tag the merge commit on `main` and push the tag:

```bash
git tag -a v2.0.0 -m 'v2.0.0'
git push origin v2.0.0
```

The proxy picks the module up from the tag, so `go get
github.com/runpod/go-sdk/v2@v2.0.0` resolves within a few minutes and
pkg.go.dev indexes it shortly after. A tag is immutable once fetched: a
mistake needs a new patch version, never a retag.

## Reference

Full API reference: <https://pkg.go.dev/github.com/runpod/go-sdk/v2>.

## License

[Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0) — see [LICENSE](LICENSE).
