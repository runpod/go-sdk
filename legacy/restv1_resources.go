package legacy

import (
	"context"
	"fmt"
)

// PodV1 is the GET /pods/{id} subset behind runpod_pod's machine_id,
// docker_entrypoint, docker_start_cmd and volume_encrypted. None of them was
// ever a create input in v1.0.8 (the create body never carried them), so they
// are read-back only. The other two v1 fields the 1.x provider read, costPerHr
// and memoryInGb, come from REST v2 as cost and memory_in_gb and are not read
// here.
//
// The scalars are pointers so an absent field stays distinguishable from a
// reported empty one: v1.0.8 set machine_id only for a present, non-empty
// machineId and volume_encrypted only for a present volumeEncrypted, leaving
// the attribute null otherwise, and a CPU or cluster pod carries neither.
type PodV1 struct {
	MachineID        *string  `json:"machineId"`
	DockerEntrypoint []string `json:"dockerEntrypoint"`
	DockerStartCmd   []string `json:"dockerStartCmd"`
	VolumeEncrypted  *bool    `json:"volumeEncrypted"`
}

// GetPodV1 reads the v1 pod fields for id. A pod the v1 route does not know,
// such as a CPU or cluster pod, comes back as *NotFoundError.
func (c *Client) GetPodV1(ctx context.Context, id string) (*PodV1, error) {
	var pod PodV1
	if err := c.getInto(ctx, resourcePath("pods", id), nil, &pod); err != nil {
		return nil, err
	}
	return &pod, nil
}

// EndpointV1 is the /endpoints/{id} subset behind runpod_serverless_endpoint's
// template_version, users, version and workers. computeType is not read: the
// provider derives compute_type from the GPU or CPU block REST v2 reports and
// never lets the legacy value contradict it.
type EndpointV1 struct {
	TemplateVersion *int
	// UserID is not a pointer: every /endpoints/{id} response carries userId,
	// so there is no absent case to distinguish.
	UserID  string
	Version *int
	Workers []EndpointWorkerV1
}

// EndpointWorkerV1 is one element of the v1 workers array.
//
// Every field is a pointer because v1.0.8 set each worker attribute only for a
// field the response carried and left it null otherwise. The two millisecond
// counters are float64 for the same reason restv1_billing.go gives: v1.0.8 read
// them as JSON numbers and truncated (`workerMap["uptimeMs"].(float64)`), so a
// fractional value is data to round rather than a decode error that would fail
// an apply whose write already landed. The mapping layer truncates.
type EndpointWorkerV1 struct {
	ID         *string  `json:"id"`
	PodID      *string  `json:"podId"`
	Status     *string  `json:"status"`
	UptimeMs   *float64 `json:"uptimeMs"`
	LastBusyMs *float64 `json:"lastBusyMs"`
	StartTime  *string  `json:"startTime"`
}

// endpointV1Wire exists because template_version comes from the nested
// template.version, not a top-level field.
type endpointV1Wire struct {
	UserID   string `json:"userId"`
	Version  *int   `json:"version"`
	Template *struct {
		Version *int `json:"version"`
	} `json:"template"`
	Workers []EndpointWorkerV1 `json:"workers"`
}

func (w endpointV1Wire) endpoint() *EndpointV1 {
	endpoint := &EndpointV1{
		UserID:  w.UserID,
		Version: w.Version,
		Workers: w.Workers,
	}
	if w.Template != nil {
		endpoint.TemplateVersion = w.Template.Version
	}
	return endpoint
}

// GetEndpointV1 reads the v1 serverless-endpoint fields for id. The route
// leaves workers out unless includeWorkers=true is on the query string, so the
// parameter is always sent.
func (c *Client) GetEndpointV1(ctx context.Context, id string) (*EndpointV1, error) {
	var wire endpointV1Wire
	params := map[string]string{"includeWorkers": "true"}
	if err := c.getInto(ctx, resourcePath("endpoints", id), params, &wire); err != nil {
		return nil, err
	}
	return wire.endpoint(), nil
}

// EndpointPatchV1 carries the two create-and-update inputs REST v2 has no
// field for. A nil field is not sent. computeType is not one of them: REST v2
// fixes the compute family through the GPU or CPU block, so sending it here
// could change nothing and could only fail the apply.
//
// v1.0.8 PATCHed /endpoints/{id} with the whole configuration (templateId,
// name, workersMin, scalerType, env and the rest), so a body carrying only
// these two keys is a shape that release never sent: this shim relies on the
// route merging a partial body. The live route is not guaranteed to, so treat a
// failure here as a warning on those two attributes rather than a failed
// write; the rest of the endpoint is written through REST v2.
type EndpointPatchV1 struct {
	CpuFlavorPriority *string `json:"cpuFlavorPriority,omitempty"`
	GpuTypePriority   *string `json:"gpuTypePriority,omitempty"`
}

// IsEmpty reports whether the patch would send nothing, so a caller can skip
// the request entirely.
func (p EndpointPatchV1) IsEmpty() bool {
	return p.CpuFlavorPriority == nil && p.GpuTypePriority == nil
}

// PatchEndpointV1 writes the two v1-only endpoint inputs and returns the
// route's read-back. An empty patch is an error rather than an empty request.
// The v1 spec gives this route no includeWorkers parameter, so the read-back
// is not relied on for workers; GetEndpointV1 asks for them.
func (c *Client) PatchEndpointV1(ctx context.Context, id string, patch EndpointPatchV1) (*EndpointV1, error) {
	if patch.IsEmpty() {
		return nil, fmt.Errorf("patching endpoint %s: nothing to send", id)
	}
	var wire endpointV1Wire
	if err := c.patchInto(ctx, resourcePath("endpoints", id), patch, &wire); err != nil {
		return nil, err
	}
	return wire.endpoint(), nil
}

// TemplateV1 is the /templates/{id} subset behind runpod_template's
// docker_entrypoint, readme, earned, is_runpod and runtime_in_min.
//
// Every field is a pointer, or a nil slice, so a caller can tell a field the
// response omitted from one it reported empty or zero: a PATCH answering with a
// partial body, or with no body at all, must leave the value alone rather
// than reporting a configured readme as cleared or a template's earnings as
// zero. runtimeInMin is float64 because v1.0.8 read it as a JSON number and
// truncated (`result["runtimeInMin"].(float64)`); the mapping layer truncates.
type TemplateV1 struct {
	DockerEntrypoint []string `json:"dockerEntrypoint"`
	Readme           *string  `json:"readme"`
	Earned           *float64 `json:"earned"`
	IsRunpod         *bool    `json:"isRunpod"`
	RuntimeInMin     *float64 `json:"runtimeInMin"`
}

// GetTemplateV1 reads the v1 template fields for id.
func (c *Client) GetTemplateV1(ctx context.Context, id string) (*TemplateV1, error) {
	var template TemplateV1
	if err := c.getInto(ctx, resourcePath("templates", id), nil, &template); err != nil {
		return nil, err
	}
	return &template, nil
}

// TemplatePatchV1 carries the two template inputs REST v2 has no field for. A
// nil field is not sent; a non-nil empty slice or string is, so a caller can
// clear either one.
//
// As with EndpointPatchV1: v1.0.8 PATCHed /templates/{id} with the whole
// configuration (name, imageName, category, containerDiskInGb, env, ports and
// the rest), so the two-key body here relies on the route merging a partial
// one, which the live route does not guarantee.
type TemplatePatchV1 struct {
	DockerEntrypoint *[]string `json:"dockerEntrypoint,omitempty"`
	Readme           *string   `json:"readme,omitempty"`
}

// IsEmpty reports whether the patch would send nothing, so a caller can skip
// the request entirely.
func (p TemplatePatchV1) IsEmpty() bool {
	return p.DockerEntrypoint == nil && p.Readme == nil
}

// PatchTemplateV1 writes the two v1-only template inputs and returns the
// route's read-back. An empty patch is an error rather than an empty request.
func (c *Client) PatchTemplateV1(ctx context.Context, id string, patch TemplatePatchV1) (*TemplateV1, error) {
	if patch.IsEmpty() {
		return nil, fmt.Errorf("patching template %s: nothing to send", id)
	}
	var template TemplateV1
	if err := c.patchInto(ctx, resourcePath("templates", id), patch, &template); err != nil {
		return nil, err
	}
	return &template, nil
}
