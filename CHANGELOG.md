# Changelog

This file is maintained by hand, newest release first. See the Releasing
section of [README.md](README.md) for the tagging procedure.

## Unreleased

- `runpod.BuildVersion()`: the module version from Go's build information
  (`Version` plus `-dev` when there is none), now sent in the `User-Agent`
  instead of `Version`.
- `User-Agent` is `runpod-go-sdk/<version> (<os>; <arch>) go<release>`, plus
  `Integration/<name>` from the new `WithIntegration` option and/or
  `RUNPOD_UA_INTEGRATION`, and `(via <agent>)` when an AI coding agent is
  detected, with the agent names runpodctl and runpod-python use.
- `New` now reads `RUNPOD_API_BASE_URL` (`WithBaseURL` still wins); before, only
  the live test did. If it is set for other tools, SDK requests go there too. A
  trailing `/v2` is dropped; another version segment or a URL that is not
  absolute http(s) is an error.
- `Accept: application/json, application/problem+json` on every request,
  `text/event-stream` on the two log-stream operations.
- `WithSessionID` sends `X-Runpod-Session-Id` on every request. Off by default.
- `WithTimeout` (default 60s, 0 for none) bounds a call, retries included.
  `WithTransport` replaces only the `http.RoundTripper` beneath the retry layer.
- `Retry-After` is honored up to 60s, or just under a shorter timeout: 59s with
  the default 60s timeout (was 60s).
- `transport.RateLimit` paces requests on the API's `RateLimit` header; the
  default client uses it.
- `runpod.APIError` carries an error response's status and problem details
  (`Title`, `Detail`, `Errors`, raw `Body`); `runpod.CheckResponse` returns one
  for any non-2xx result.

## v2.2.0

Regenerated from the 2026-11-13 released REST v2 spec, the profile a production
build serves at `/v2/openapi.yaml`: 108 operations across fourteen tags, up from
67 across twelve. Nothing was removed.

### Added

- Cost centers on `client.CostCenters`: `ListCostCenters`, `CreateCostCenter`,
  `GetCostCenter`, `UpdateCostCenter`, `DeleteCostCenter`, and
  `ListCostCenterResources`, `AttachCostCenterResource`,
  `DetachCostCenterResource` over `/v2/cost-centers`. A cost center groups
  resources so their spend is billed and reported together.
- Organization administration on `client.Organization`, twenty-two operations
  over `/v2/organization`: users (`ListOrganizationUsers`, `GetOrganizationUser`,
  `UpdateOrganizationUser`, `DeleteOrganizationUser`), invites
  (`ListOrganizationInvites`, `CreateOrganizationInvite`, `GetOrganizationInvite`,
  `DeleteOrganizationInvite`), roles (`ListOrganizationRoles`,
  `CreateOrganizationRole`, `GetOrganizationRole`, `UpdateOrganizationRole`,
  `DeleteOrganizationRole`, `GetOrganizationRoleCapabilities`) and groups
  (`ListOrganizationGroups`, `CreateOrganizationGroup`, `GetOrganizationGroup`,
  `UpdateOrganizationGroup`, `DeleteOrganizationGroup`,
  `ListOrganizationGroupMembers`, `AddOrganizationGroupMember`,
  `RemoveOrganizationGroupMember`).
- Queued pods on `client.Pods`: `ListQueuedPods`, `GetQueuedPod` and
  `DeleteQueuedPod` over `/v2/queued-pods`, for a pod waiting on capacity, plus
  `ExecPod` to run a command in a running pod.
- Endpoint metrics on `client.Serverless`: `GetEndpointRequestMetrics`,
  `GetEndpointColdStartMetrics` and `GetEndpointWorkerMetrics`. Their `interval`
  is an enum of durations (`1m` through `1d`), and they page over time buckets
  rather than over a list.
- `client.Account.GetAccount` (`GET /v2/account`): the authenticated account.
- `lib/signup`: `CreateAuthenticationAttempt`, `VerifyAuthenticationAttempt` and
  `ResendAuthenticationAttempt` over `/v2/signup`. These declare no auth, and a
  caller making them has no key yet, so they are **not** a field on `Client`:
  build one with `signup.NewClientWithResponses(baseURL)` and send no key.
- Cursor pagination grew from nine operations to seventeen: the five new list
  operations above (`ListQueuedPods`, `ListCostCenters`,
  `ListCostCenterResources`, `ListOrganizationGroups`,
  `ListOrganizationGroupMembers`) and the three metrics reads join it. An
  omitted `limit` returns the first 1000 items, 100 for the builds list and the
  two organization group lists, and 200 for the audit logs.

### Changed (breaking for callers)

- `types.UpdatePodRequest.Mounts` is a `*types.UpdatePodMounts` instead of a
  `*types.Mounts`, so code that sets it stops compiling. The request body is no
  longer the pod's whole mount set: each entry names one volume by `volumeId`,
  a listed volume the pod already has is moved, and `remove: true` unmounts one.
  Limits are checked against the pod as it would be after the request.

### Spec patch layer

- The `BaseMounts` public-description edit is retired: upstream rewrote that
  text itself and it no longer names the handler. The `Mounts` and
  `CreatePodRequest` anchors were re-derived against the new wording.
- `EndpointMetricsInterval` is named in `ENUM_VARNAMES`, so its constants spell
  the unit out (`EndpointMetricsInterval1Minute`): `1m` reads as both minute and
  month, and the bare value would generate `serverless.1m`.
- The enum-coverage test no longer walks into response headers. oapi-codegen
  types those as plain strings, so an enum there generates no identifier to name.

## v2.1.0

Regenerated from the 2026-10-26 released REST v2 spec, the profile a
production build serves at `/v2/openapi.yaml`: 67 operations across twelve
tags.

### Added

- Global volumes on `client.GlobalVolumes`: `ListGlobalVolumes`,
  `CreateGlobalVolume`, `GetGlobalVolume`, `UpdateGlobalVolume`,
  `DeleteGlobalVolume` over `/v2/global-volumes`. A global volume mounts into
  pods and serverless workers in any data center; create takes only a name.
- Global volume mounts: `types.Mounts.Global` on pod create, update and reads,
  and `Mounts` (`types.EndpointMountsInput` on create and update,
  `types.EndpointMounts` on reads) on serverless endpoints. The endpoint
  `NetworkVolumes` fields are marked deprecated in favour of `Mounts.Network`.
- `client.Organization.GetOrganization` (`GET /v2/organization`): the caller's
  current organization.
- `client.AuditLogs.ListAuditLogs` (`GET /v2/audit-logs`): the account's audit
  trail, cursor-paginated (page size up to 200), with actor, action, time-range
  and text filters.
- 413 responses: `RequestTooLargeError` on the write operations (a request
  body over the route's limit, 100 KiB on pod create) and
  `ResponseTooLargeError` on the nine paginated list operations (a page too
  large to return; lower `limit`). Each result type gains
  `ApplicationproblemJSON413`.

### Changed (breaking for callers)

- `Clusters.ListClusters` and `NetworkVolumes.ListNetworkVolumes` are now
  cursor-paginated: their client methods and `NewList*Request` constructors
  take a `*ListClustersParams` / `*ListNetworkVolumesParams` argument (pass
  `nil` for the defaults), and the responses carry `pagination`. An omitted
  `limit` returns the first 1000 items, so a caller that wants every cluster or
  volume follows `pagination.nextCursor` until `hasNextPage` is false.
- `types.CreateClusterRequest.Mounts` is a `*types.ClusterMounts` (persistent
  or network, no global) instead of a `*types.Mounts`. Both carry the same
  `Persistent` and `Network` fields.

### Removed (breaking for callers)

- The REST v1 half of the deprecated `legacy` package, ahead of the REST v1
  host's retirement: `DefaultRestV1URL`, `Config.RestV1URL`,
  `Client.RestV1URL`, `Client.Get`, the typed pod, template, endpoint and
  billing routes (`GetPodV1`, `GetTemplateV1`, `PatchTemplateV1`,
  `GetEndpointV1`, `PatchEndpointV1`, `ListPodBillingV1`,
  `ListNetworkVolumeBillingV1`, `ListEndpointBillingV1` and their request and
  record types), and `DecodeError` / `IsDecodeFailure`, which only those
  routes returned. The GraphQL half (`New`, `Config`, `Client.Query`,
  `Client.GraphQLURL`, `NotFoundError`, `IsNotFound`) is unchanged.

### Spec patch layer

- The delegation auth-responses patch is retired: the served spec now declares
  401 and 403 on the three ECR-delegation operations itself.
- The public-description edit for `Mounts` moved to `BaseMounts`, where the
  served spec now keeps that text; `Mounts` gets the matching edit for its new
  wording.
- Strings holding a YAML line-break character other than `\n` (the global
  volume name patterns carry U+0085) are written double-quoted, so the
  round-trip check passes and the character survives into the vendored spec.

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
