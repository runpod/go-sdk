package legacy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// The typed REST v1 routes behind the deprecated pod, endpoint and template
// attributes: the wire names come from the v1.0.8 trace, so these pin the JSON
// each call actually sends and reads rather than the Go shape alone.

func TestGetPodV1ReadsTheShimFields(t *testing.T) {
	got := newCapture()
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		got.set("path", r.URL.Path)
		got.set("auth", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"machineId":"machine-1","dockerEntrypoint":["/bin/bash"],`+
			`"dockerStartCmd":["-c","sleep infinity"],"volumeEncrypted":true,"costPerHr":0.35}`)
	})

	pod, err := testClient(t, srv.URL).GetPodV1(context.Background(), "pod_abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.get("path") != "/pods/pod_abc" {
		t.Errorf("path = %q, want /pods/pod_abc", got.get("path"))
	}
	if got.get("auth") != "Bearer test-key" {
		t.Errorf("Authorization = %q", got.get("auth"))
	}
	if pod.MachineID == nil || *pod.MachineID != "machine-1" || pod.VolumeEncrypted == nil || !*pod.VolumeEncrypted {
		t.Errorf("pod = %+v", pod)
	}
	if len(pod.DockerEntrypoint) != 1 || len(pod.DockerStartCmd) != 2 {
		t.Errorf("command lists = %+v", pod)
	}
}

func TestGetPodV1ReportsANotFound(t *testing.T) {
	// A CPU pod or a cluster pod the v1 route does not know: the caller tells
	// this apart from a failed read and stays quiet about it.
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"pod not found"}`)
	})

	_, err := testClient(t, srv.URL).GetPodV1(context.Background(), "pod_abc")
	if !IsNotFound(err) {
		t.Fatalf("error = %v, want a *NotFoundError", err)
	}
	if !strings.Contains(err.Error(), "/pods/pod_abc") {
		t.Errorf("error = %q, want the route", err)
	}
}

func TestGetEndpointV1UnwrapsTheNestedTemplateVersion(t *testing.T) {
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"computeType":"GPU","userId":"user-1","version":3,`+
			`"template":{"version":7},"workers":[{"id":"worker-1","podId":"pod-1","status":"RUNNING",`+
			`"uptimeMs":1200,"lastBusyMs":45,"startTime":"2026-09-17T09:00:00Z"}]}}`)
	})

	endpoint, err := testClient(t, srv.URL).GetEndpointV1(context.Background(), "ep_abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// computeType is in the fixture and in no struct: the provider derives
	// compute_type from what REST v2 reports, so the field is ignored here
	// rather than decoded.
	if endpoint.UserID != "user-1" {
		t.Errorf("endpoint = %+v", endpoint)
	}
	if endpoint.Version == nil || *endpoint.Version != 3 {
		t.Errorf("version = %v", endpoint.Version)
	}
	if endpoint.TemplateVersion == nil || *endpoint.TemplateVersion != 7 {
		t.Errorf("template version = %v, want the nested template.version", endpoint.TemplateVersion)
	}
	if len(endpoint.Workers) != 1 || endpoint.Workers[0].UptimeMs == nil || *endpoint.Workers[0].UptimeMs != 1200 {
		t.Errorf("workers = %+v", endpoint.Workers)
	}
}

func TestGetEndpointV1AsksForWorkers(t *testing.T) {
	// The v1 route answers workers only when includeWorkers=true is on the
	// query string; without it the field is absent and every worker attribute
	// would read empty.
	got := newCapture()
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		got.set("path", r.URL.Path)
		got.set("includeWorkers", r.URL.Query().Get("includeWorkers"))
		body := `{"id":"ep_abc","computeType":"GPU","userId":"user-1","version":3,"templateId":"tpl-1"`
		if r.URL.Query().Get("includeWorkers") == "true" {
			body += `,"workers":[{"id":"worker-1","podId":"pod-1","status":"RUNNING",` +
				`"uptimeMs":1200,"lastBusyMs":45,"startTime":"2026-09-17T09:00:00Z"}]`
		}
		_, _ = io.WriteString(w, body+"}")
	})

	endpoint, err := testClient(t, srv.URL).GetEndpointV1(context.Background(), "ep_abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.get("path") != "/endpoints/ep_abc" {
		t.Errorf("path = %q, want /endpoints/ep_abc", got.get("path"))
	}
	if got.get("includeWorkers") != "true" {
		t.Errorf("includeWorkers = %q, want true", got.get("includeWorkers"))
	}
	if len(endpoint.Workers) != 1 || endpoint.Workers[0].ID == nil || *endpoint.Workers[0].ID != "worker-1" {
		t.Errorf("workers = %+v, want the one worker the route reports", endpoint.Workers)
	}
}

func TestGetEndpointV1LeavesAnAbsentTemplateNull(t *testing.T) {
	// An endpoint built from an image carries no template object at all.
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"computeType":"CPU","userId":"user-1"}`)
	})

	endpoint, err := testClient(t, srv.URL).GetEndpointV1(context.Background(), "ep_abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint.TemplateVersion != nil || endpoint.Version != nil || len(endpoint.Workers) != 0 {
		t.Errorf("endpoint = %+v, want the absent fields nil", endpoint)
	}
}

func TestPatchEndpointV1SendsOnlyTheSetFields(t *testing.T) {
	got := newCapture()
	var gotBody atomic.Value
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		got.set("method", r.Method)
		got.set("path", r.URL.Path)
		got.set("contentType", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		var sent map[string]any
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Errorf("request body %q: %v", body, err)
		}
		gotBody.Store(sent)
		_, _ = io.WriteString(w, `{"computeType":"GPU","userId":"user-1","version":4,"workers":[]}`)
	})

	priority := "availability"
	endpoint, err := testClient(t, srv.URL).PatchEndpointV1(context.Background(), "ep_abc",
		EndpointPatchV1{GpuTypePriority: &priority})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.get("method") != http.MethodPatch || got.get("path") != "/endpoints/ep_abc" {
		t.Errorf("%s %s, want PATCH /endpoints/ep_abc", got.get("method"), got.get("path"))
	}
	if got.get("contentType") != "application/json" {
		t.Errorf("Content-Type = %q", got.get("contentType"))
	}
	sent, _ := gotBody.Load().(map[string]any)
	if len(sent) != 1 || sent["gpuTypePriority"] != "availability" {
		t.Errorf("body = %v, want only the field that was set", sent)
	}
	if endpoint.Version == nil || *endpoint.Version != 4 {
		t.Errorf("the patch response must carry the read-backs: %+v", endpoint)
	}
}

func TestPatchEndpointV1RefusesAnEmptyPatch(t *testing.T) {
	srv := testServer(t, func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("an empty patch must not reach the host: %s %s", r.Method, r.URL.Path)
	})

	if _, err := testClient(t, srv.URL).PatchEndpointV1(context.Background(), "ep_abc", EndpointPatchV1{}); err == nil {
		t.Fatal("expected an error for a patch with nothing to send")
	}
}

func TestGetTemplateV1ReadsTheShimFields(t *testing.T) {
	got := newCapture()
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		got.set("path", r.URL.Path)
		_, _ = io.WriteString(w, `{"dockerEntrypoint":["/bin/bash"],"readme":"# trainer",`+
			`"earned":1.25,"isRunpod":true,"runtimeInMin":90}`)
	})

	template, err := testClient(t, srv.URL).GetTemplateV1(context.Background(), "tpl_abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.get("path") != "/templates/tpl_abc" {
		t.Errorf("path = %q, want /templates/tpl_abc", got.get("path"))
	}
	if template.Readme == nil || *template.Readme != "# trainer" {
		t.Errorf("readme = %v", template.Readme)
	}
	if template.Earned == nil || *template.Earned != 1.25 || template.IsRunpod == nil || !*template.IsRunpod ||
		template.RuntimeInMin == nil || *template.RuntimeInMin != 90 {
		t.Errorf("template = %+v", template)
	}
}

func TestPatchTemplateV1RoundTripsTheReadme(t *testing.T) {
	var gotBody atomic.Value
	srv := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var sent map[string]any
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Errorf("request body %q: %v", body, err)
		}
		gotBody.Store(sent)
		// A partial response: the route echoes the counters and not the
		// fields it was sent.
		_, _ = io.WriteString(w, `{"earned":0,"isRunpod":false,"runtimeInMin":0}`)
	})

	readme := ""
	template, err := testClient(t, srv.URL).PatchTemplateV1(context.Background(), "tpl_abc",
		TemplatePatchV1{Readme: &readme})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// An empty readme clears the text, so it is sent rather than omitted.
	sent, _ := gotBody.Load().(map[string]any)
	if len(sent) != 1 {
		t.Errorf("body = %v, want only readme", sent)
	}
	if value, ok := sent["readme"]; !ok || value != "" {
		t.Errorf("body = %v, want the empty readme sent", sent)
	}
	if template.Readme != nil {
		t.Errorf("readme = %v, want nil so the caller keeps what it planned", *template.Readme)
	}
}

func TestPatchTemplateV1AcceptsASuccessWithNoBody(t *testing.T) {
	// A write that answers 204 landed; failing the apply over the status code
	// or over an empty body would report a change the host already made as a
	// failure. Live v1 PATCH semantics are unverified, so this is tolerance.
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	readme := "# trainer"
	template, err := testClient(t, srv.URL).PatchTemplateV1(context.Background(), "tpl_abc",
		TemplatePatchV1{Readme: &readme})
	if err != nil {
		t.Fatalf("a 204 must not fail the write: %v", err)
	}
	if template.Readme != nil {
		t.Errorf("readme = %v, want nil so the caller keeps what it planned", *template.Readme)
	}
}

func TestFractionalRestV1NumbersDecode(t *testing.T) {
	// v1.0.8 read every one of these as a JSON float64 and truncated, so a
	// fractional value is data to round. Decoding it into an int would fail the
	// call — and on a PATCH that is an apply killed by a write that landed.
	endpointSrv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"userId":"user-1","workers":[{"id":"w1","uptimeMs":1234.5,"lastBusyMs":45.9}]}`)
	})

	priority := "availability"
	endpoint, err := testClient(t, endpointSrv.URL).PatchEndpointV1(context.Background(), "ep_abc",
		EndpointPatchV1{GpuTypePriority: &priority})
	if err != nil {
		t.Fatalf("a fractional uptimeMs must not fail a write that landed: %v", err)
	}
	if len(endpoint.Workers) != 1 || endpoint.Workers[0].UptimeMs == nil || *endpoint.Workers[0].UptimeMs != 1234.5 {
		t.Fatalf("workers = %+v", endpoint.Workers)
	}

	templateSrv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"runtimeInMin":90.5}`)
	})

	readme := "# trainer"
	template, err := testClient(t, templateSrv.URL).PatchTemplateV1(context.Background(), "tpl_abc",
		TemplatePatchV1{Readme: &readme})
	if err != nil {
		t.Fatalf("a fractional runtimeInMin must not fail a write that landed: %v", err)
	}
	if template.RuntimeInMin == nil || *template.RuntimeInMin != 90.5 {
		t.Fatalf("runtimeInMin = %v", template.RuntimeInMin)
	}
}

func TestUnreadableSuccessBodyIsADecodeError(t *testing.T) {
	// A 2xx body that is not the expected object: the host took the change and
	// answered something this client cannot read, which the caller has to tell
	// apart from a write that never landed.
	srv := testServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `["not","the expected object"]`)
	})

	readme := "# trainer"
	_, err := testClient(t, srv.URL).PatchTemplateV1(context.Background(), "tpl_abc",
		TemplatePatchV1{Readme: &readme})
	if !IsDecodeFailure(err) {
		t.Fatalf("error = %v, want a *DecodeError", err)
	}
	if IsNotFound(err) {
		t.Fatalf("a decode failure is not a 404: %v", err)
	}
}
