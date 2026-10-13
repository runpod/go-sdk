package main

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// packageName and pathItemTags decide the entire public package layout, and the
// only other record of those decisions is the generated output itself.

func TestPackageName(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		want string
	}{
		{"Pods", "pods"},
		{"Network Volumes", "networkvolumes"},
		{"Serverless", "serverless"},
		{"Other", "other"},
		{"GPU-Types", "gputypes"},
		{"Registries (ECR)", "registriesecr"},
		{"S3 v2", "s3v2"},
	} {
		if got := packageName(tc.tag); got != tc.want {
			t.Errorf("packageName(%q) = %q, want %q", tc.tag, got, tc.want)
		}
	}
}

func TestPathItemTags(t *testing.T) {
	multi := map[string]any{
		"get":        map[string]any{"tags": []any{"Clusters", "Pods"}},
		"delete":     map[string]any{"tags": []any{"Pods"}},
		"parameters": []any{map[string]any{"name": "id"}},
		"summary":    "not an operation",
	}
	if got := pathItemTags(multi); !slices.Equal(got, []string{"Clusters", "Pods"}) {
		t.Errorf("tags = %v, want each tag once across both operations", got)
	}

	untagged := map[string]any{"get": map[string]any{"operationId": "listThings"}}
	if got := pathItemTags(untagged); !slices.Equal(got, []string{"Other"}) {
		t.Errorf("tags = %v, want the Other fallback so nothing is silently lost", got)
	}

	if got := pathItemTags(map[string]any{}); len(got) != 0 {
		t.Errorf("tags = %v, want none for a path item with no operations", got)
	}
}

// The reference rewrite is what points the per-tag packages at lib/types: a
// reference it misses generates a type the per-tag package declares itself,
// which is the duplication lib/types exists to remove.

func TestRewriteSharedRefs(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   any
		want any
	}{
		{
			name: "a response content schema",
			in:   map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Pod"}},
			want: map[string]any{"schema": map[string]any{"$ref": "components.yaml#/components/schemas/Pod"}},
		},
		{
			name: "a parameter in an operation's list",
			in:   map[string]any{"parameters": []any{map[string]any{"$ref": "#/components/parameters/BillingLastN"}}},
			want: map[string]any{"parameters": []any{map[string]any{"$ref": "components.yaml#/components/parameters/BillingLastN"}}},
		},
		{
			name: "a response header",
			in:   map[string]any{"headers": map[string]any{"RateLimit": map[string]any{"$ref": "#/components/headers/RateLimit"}}},
			want: map[string]any{"headers": map[string]any{"RateLimit": map[string]any{"$ref": "components.yaml#/components/headers/RateLimit"}}},
		},
		{
			name: "an allOf member, array items and additionalProperties",
			in: map[string]any{
				"allOf":                []any{map[string]any{"$ref": "#/components/schemas/BaseGpuConfig"}},
				"items":                map[string]any{"$ref": "#/components/schemas/Mounts"},
				"additionalProperties": map[string]any{"$ref": "#/components/schemas/PodStatus"},
			},
			want: map[string]any{
				"allOf":                []any{map[string]any{"$ref": "components.yaml#/components/schemas/BaseGpuConfig"}},
				"items":                map[string]any{"$ref": "components.yaml#/components/schemas/Mounts"},
				"additionalProperties": map[string]any{"$ref": "components.yaml#/components/schemas/PodStatus"},
			},
		},
		{
			name: "a oneOf branch",
			in:   map[string]any{"oneOf": []any{map[string]any{"$ref": "#/components/schemas/QueueDelayScaling"}}},
			want: map[string]any{"oneOf": []any{map[string]any{"$ref": "components.yaml#/components/schemas/QueueDelayScaling"}}},
		},
		{
			name: "a response, which is inlined instead",
			in:   map[string]any{"$ref": "#/components/responses/UnauthorizedError"},
			want: map[string]any{"$ref": "#/components/responses/UnauthorizedError"},
		},
		{
			name: "a reference that already names a file",
			in:   map[string]any{"$ref": "components.yaml#/components/schemas/Pod"},
			want: map[string]any{"$ref": "components.yaml#/components/schemas/Pod"},
		},
		{
			name: "a value that only looks like a reference",
			in:   map[string]any{"description": "as in #/components/schemas/Pod"},
			want: map[string]any{"description": "as in #/components/schemas/Pod"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rewriteSharedRefs(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rewriteSharedRefs() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestRewriteSharedRefsLeavesItsInputAlone(t *testing.T) {
	// A path item tagged twice is rewritten once per tag, off the same spec
	// map, so a rewrite in place would hand the second tag a reference that
	// already names the file and rewrite it again.
	pathItem := map[string]any{"get": map[string]any{"responses": map[string]any{
		"200": map[string]any{"content": map[string]any{"application/json": map[string]any{
			"schema": map[string]any{"$ref": "#/components/schemas/Pod"},
		}}},
	}}}

	first := rewriteSharedRefs(pathItem)
	second := rewriteSharedRefs(pathItem)
	if !reflect.DeepEqual(first, second) {
		t.Errorf("a second rewrite of the same path item differs:\n%#v\n%#v", first, second)
	}
}

func TestInlineSharedResponses(t *testing.T) {
	// The shared responses cannot be referenced by file: oapi-codegen loses the
	// file name of a header reached through an externally referenced response
	// and emits an undefined local type for its schema. Inlining the response
	// leaves the header reference in the per-tag document, where the rewrite
	// above turns it into a reference the import mapping resolves.
	responses := map[string]any{"UnauthorizedError": map[string]any{
		"description": "Unauthorized",
		"headers": map[string]any{
			"RateLimit": map[string]any{"$ref": "#/components/headers/RateLimit"},
		},
	}}
	paths := map[string]any{"/v2/pods": map[string]any{"get": map[string]any{
		"responses": map[string]any{"401": map[string]any{"$ref": "#/components/responses/UnauthorizedError"}},
	}}}

	inlined, err := inlineSharedResponses(paths, responses)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"/v2/pods": map[string]any{"get": map[string]any{
		"responses": map[string]any{"401": map[string]any{
			"description": "Unauthorized",
			"headers": map[string]any{
				"RateLimit": map[string]any{"$ref": "components.yaml#/components/headers/RateLimit"},
			},
		}},
	}}}
	if got := rewriteSharedRefs(inlined); !reflect.DeepEqual(got, want) {
		t.Errorf("inlined and rewritten paths = %#v, want %#v", got, want)
	}
}

func TestInlineSharedResponsesRejectsAnAbsentResponse(t *testing.T) {
	_, err := inlineSharedResponses(map[string]any{"401": map[string]any{
		"$ref": "#/components/responses/Missing",
	}}, map[string]any{})
	if err == nil {
		t.Error("a reference to a response the spec does not declare was accepted")
	}
}

func TestSwapInKeepsTheOldTreeUntilTheNewOneIsInPlace(t *testing.T) {
	// swapIn is relative to the repository root, and the backup goes under
	// artifacts/, so the test runs in a temporary root of its own.
	root := t.TempDir()
	chdir(t, root)
	if err := os.MkdirAll("artifacts", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("stage", "pods"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("stage", "pods", "pods.gen.go"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("lib", "pods"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("lib", "pods", "pods.gen.go"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := swapIn("stage", "lib"); err != nil {
		t.Fatal(err)
	}

	generated, err := os.ReadFile(filepath.Join("lib", "pods", "pods.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(generated) != "new" {
		t.Errorf("lib holds %q, want the staged tree", generated)
	}
	if _, err := os.Stat(filepath.Join("artifacts", "lib.prev")); !os.IsNotExist(err) {
		t.Errorf("the backup survived the swap: %v", err)
	}
}

func TestSwapInIntoAnAbsentDestination(t *testing.T) {
	// A first run, or a run after lib/ was deleted by hand.
	root := t.TempDir()
	chdir(t, root)
	if err := os.MkdirAll("artifacts", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("stage", 0o755); err != nil {
		t.Fatal(err)
	}

	if err := swapIn("stage", "lib"); err != nil {
		t.Fatalf("swapping into an absent destination failed: %v", err)
	}
	if _, err := os.Stat("lib"); err != nil {
		t.Errorf("lib was not created: %v", err)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatal(err)
		}
	})
}
