// Command generator splits spec/openapi.yaml into one spec per OpenAPI tag plus
// one spec holding the shared components, and runs oapi-codegen (pinned via
// this module's tool directive) on each, producing one Go package per tag under
// lib/ over a single lib/types package.
//
// The components object is generated once, into lib/types: every schema,
// response, parameter and header lives there, and each per-tag spec keeps only
// its paths, with every reference into those sections rewritten to point at
// components.yaml. oapi-codegen resolves that as an external reference and the
// per-tag config's import-mapping turns it into a lib/types import, so a schema
// several tags share is one Go type rather than one copy per package.
//
// The split is generic (map-based, not typed structs) so no spec keys are
// dropped: each tag's spec keeps the full top-level document (info, servers,
// security) with paths filtered to the tag. A path item whose operations carry
// several tags lands in every one of them.
//
// This is its own module so that the code generator and its dependency tree
// stay out of the published SDK's requirements. It reads and writes relative to
// -root, which `task generate` points at the repository root:
//
//	go -C generator run . -root ..
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	specPath     = "spec/openapi.yaml"
	artifactsDir = "artifacts/api"
	libDir       = "lib"

	// Staging copies of the two generated trees, swapped in only once every tag
	// has generated cleanly. Both sit under the gitignored artifacts/ dir.
	libStageDir       = "artifacts/lib.stage"
	artifactsStageDir = "artifacts/api.stage"

	// The shared components: one spec file, one Go package, imported by every
	// per-tag package. componentsSpec is also the import-mapping key, so it is
	// the reference string the per-tag specs carry and must stay a plain file
	// name resolved next to them.
	componentsSpec  = "components.yaml"
	typesPackage    = "types"
	typesImportPath = "github.com/runpod/go-sdk/v2/lib/" + typesPackage
)

// sharedSections are the components sections that live in componentsSpec and
// generate into lib/types. Every other section (securitySchemes) generates no
// Go type and is kept in both documents so each resolves its own references.
var sharedSections = []string{"schemas", "responses", "parameters", "headers"}

// referencedSections are the shared sections a per-tag spec reaches by naming
// componentsSpec in the reference. components/responses is not one of them: see
// inlineSharedResponses.
var referencedSections = []string{"schemas", "parameters", "headers"}

var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

func main() {
	root := flag.String("root", ".", "repository root to read the spec from and write lib/ into")
	flag.Parse()
	if err := run(*root); err != nil {
		fmt.Fprintf(os.Stderr, "generator: %v\n", err)
		os.Exit(1)
	}
}

func run(root string) error {
	// Every path below is repository-relative; generatePackage runs the codegen
	// from the generator module's own directory, which is where the tool
	// directive resolves.
	generatorDir, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(root); err != nil {
		return fmt.Errorf("repository root %q: %w", root, err)
	}

	raw, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read spec: %w", err)
	}

	var spec map[string]any
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return fmt.Errorf("parse spec: %w", err)
	}

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		return fmt.Errorf("spec has no paths object")
	}
	components, ok := spec["components"].(map[string]any)
	if !ok {
		return fmt.Errorf("spec has no components object")
	}

	// The document every generated spec repeats: info, servers, security, the
	// openapi version. paths and components differ per spec, and the tag list
	// describes a whole-API document that no split spec is.
	envelope := map[string]any{}
	for key, value := range spec {
		if key != "paths" && key != "components" && key != "tags" {
			envelope[key] = value
		}
	}

	// Sections the per-tag specs keep: the ones lib/types does not own, which
	// generate no Go type but are referenced from the documents' own security
	// blocks.
	tagComponents := map[string]any{}
	for section, value := range components {
		if !slices.Contains(sharedSections, section) {
			tagComponents[section] = value
		}
	}

	tagPaths := map[string]map[string]any{}
	for path, item := range paths {
		pathItem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for _, tag := range pathItemTags(pathItem) {
			if tagPaths[tag] == nil {
				tagPaths[tag] = map[string]any{}
			}
			tagPaths[tag][path] = pathItem
		}
	}
	if len(tagPaths) == 0 {
		return fmt.Errorf("no tagged operations found in spec")
	}

	// lib/ and artifacts/api/ are wholly owned by this generator and regenerated
	// from scratch, but a codegen failure partway through must not leave lib/
	// half-populated and the repo uncompilable: build into staging trees and
	// swap them in only after the last tag succeeds.
	for _, dir := range []string{libStageDir, artifactsStageDir} {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	defer func() {
		// Both trees are gone already on the success path (renamed into place);
		// this only sweeps up after a failure, so a cleanup error has nothing
		// left to report to.
		_ = os.RemoveAll(libStageDir)
		_ = os.RemoveAll(artifactsStageDir)
	}()

	// lib/types first: every per-tag package imports it, so a failure here is a
	// failure of the whole run rather than of one tag.
	componentsDoc := map[string]any{"paths": map[string]any{}, "components": components}
	for key, value := range envelope {
		componentsDoc[key] = value
	}
	schemas, _ := components["schemas"].(map[string]any)
	responses, _ := components["responses"].(map[string]any)
	fmt.Printf("generating lib/%s (%d shared schemas)\n", typesPackage, len(schemas))
	componentsConfig := map[string]any{
		"package": typesPackage,
		"generate": map[string]any{
			"models": true,
		},
		// This document carries no paths, so every component is unreachable and
		// the default pruning pass would leave an empty package behind.
		"output-options": map[string]any{
			"skip-prune": true,
		},
	}
	if err := generatePackage(generatorDir, typesPackage, componentsSpec, componentsDoc, componentsConfig); err != nil {
		return err
	}

	tags := make([]string, 0, len(tagPaths))
	for tag := range tagPaths {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	for _, tag := range tags {
		pkg := packageName(tag)
		if pkg == typesPackage {
			return fmt.Errorf("tag %q maps to package %q, which lib/types already owns", tag, pkg)
		}
		fmt.Printf("generating lib/%s (tag %q, %d paths)\n", pkg, tag, len(tagPaths[tag]))

		inlined, err := inlineSharedResponses(tagPaths[tag], responses)
		if err != nil {
			return fmt.Errorf("tag %q: %w", tag, err)
		}
		tagSpec := map[string]any{
			// The shared components are gone from this document, so every
			// reference into them has to name the file they moved to.
			"paths":      rewriteSharedRefs(inlined),
			"components": tagComponents,
		}
		for key, value := range envelope {
			tagSpec[key] = value
		}

		config := map[string]any{
			"package": pkg,
			"generate": map[string]any{
				"client": true,
				"models": true,
			},
			// The wrappers are named "<OperationId>Result", not the default
			// "<OperationId>Response". That started as a collision fix — the
			// spec names its list schemas "<OperationId>Response" and both
			// landed in one package — and the schemas moving to lib/types
			// settled the collision, but the wrapper names are part of the
			// frozen v2 surface, so they stay as they are.
			"output-options": map[string]any{
				"response-type-suffix": "Result",
			},
			// The key is the reference string as the spec writes it; the value
			// is the package those components were generated into.
			"import-mapping": map[string]any{
				componentsSpec: typesImportPath,
			},
		}
		if err := generatePackage(generatorDir, pkg, pkg+".yaml", tagSpec, config); err != nil {
			return err
		}
	}

	for _, swap := range [][2]string{{libStageDir, libDir}, {artifactsStageDir, artifactsDir}} {
		if err := swapIn(swap[0], swap[1]); err != nil {
			return err
		}
	}
	return nil
}

// generatePackage writes one spec and its codegen config into the artifacts
// staging tree and runs oapi-codegen over them, into lib/<pkg>/<pkg>.gen.go in
// the lib staging tree. Both specs sit directly under the artifacts dir so a
// per-tag spec resolves componentsSpec as a sibling file, which is what makes
// the import-mapping key a plain file name.
func generatePackage(generatorDir, pkg, specName string, spec, config map[string]any) error {
	if err := os.MkdirAll(filepath.Join(libStageDir, pkg), 0o755); err != nil {
		return err
	}

	specFile := filepath.Join(artifactsStageDir, specName)
	if err := writeYAML(specFile, spec); err != nil {
		return err
	}
	// No output key in the config: the destination is passed with -o, so this
	// file records the codegen settings without being a script that writes into
	// the tracked lib/ tree behind `task generate`.
	configFile := filepath.Join(artifactsStageDir, strings.TrimSuffix(specName, ".yaml")+".config.yaml")
	if err := writeYAML(configFile, config); err != nil {
		return err
	}

	// `go tool oapi-codegen` resolves the tool directive from the generator
	// module, so the command runs in that directory and every path it is handed
	// is absolute.
	outputFile, err := filepath.Abs(filepath.Join(libStageDir, pkg, pkg+".gen.go"))
	if err != nil {
		return err
	}
	absConfig, err := filepath.Abs(configFile)
	if err != nil {
		return err
	}
	absSpec, err := filepath.Abs(specFile)
	if err != nil {
		return err
	}
	cmd := exec.Command("go", "tool", "oapi-codegen", "-config", absConfig, "-o", outputFile, absSpec)
	cmd.Dir = generatorDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("oapi-codegen failed for package %q:\n%s", pkg, out)
	}
	return nil
}

// rewriteSharedRefs returns node with every local reference into a section
// lib/types owns rewritten to name componentsSpec, wherever it appears — a
// parameter, a request body, a response, an allOf or oneOf member, an array's
// items, an additionalProperties schema. References into the sections that stay
// in the per-tag spec are left alone.
func rewriteSharedRefs(node any) any {
	switch node := node.(type) {
	case map[string]any:
		rewritten := make(map[string]any, len(node))
		for key, value := range node {
			if ref, ok := value.(string); key == "$ref" && ok && isSharedRef(ref) {
				rewritten[key] = componentsSpec + ref
				continue
			}
			rewritten[key] = rewriteSharedRefs(value)
		}
		return rewritten
	case []any:
		rewritten := make([]any, len(node))
		for i, value := range node {
			rewritten[i] = rewriteSharedRefs(value)
		}
		return rewritten
	default:
		return node
	}
}

// isSharedRef reports whether ref is a local reference into one of the
// components sections a per-tag spec reaches through componentsSpec.
func isSharedRef(ref string) bool {
	for _, section := range referencedSections {
		if strings.HasPrefix(ref, "#/components/"+section+"/") {
			return true
		}
	}
	return false
}

// inlineSharedResponses returns node with every reference into
// components/responses replaced by the response it names, so the per-tag specs
// carry those responses rather than referencing componentsSpec for them.
//
// They have to: oapi-codegen qualifies a response header's schema with the
// header's own reference (pkg/codegen/operations.go, ensureExternalRefsInSchema
// on header.Ref), and a header reached through an externally referenced
// response names its schema inside the shared document — "#/components/schemas/
// RateLimitHeader" — so no file name is left to map and the generated Headers
// struct points at a local type the per-tag package never declares:
//
//	lib/clusters/clusters.gen.go:668:19: undefined: RateLimitHeader
//
// Inlining the response keeps its header references in the per-tag document,
// where they name componentsSpec and the import mapping resolves them. The
// responses themselves still generate, once, into lib/types.
func inlineSharedResponses(node any, responses map[string]any) (any, error) {
	switch node := node.(type) {
	case map[string]any:
		if ref, ok := node["$ref"].(string); ok {
			if name, isResponse := strings.CutPrefix(ref, "#/components/responses/"); isResponse {
				response, ok := responses[name]
				if !ok {
					return nil, fmt.Errorf("reference %q names no response in components", ref)
				}
				return response, nil
			}
		}
		inlined := make(map[string]any, len(node))
		for key, value := range node {
			resolved, err := inlineSharedResponses(value, responses)
			if err != nil {
				return nil, err
			}
			inlined[key] = resolved
		}
		return inlined, nil
	case []any:
		inlined := make([]any, len(node))
		for i, value := range node {
			resolved, err := inlineSharedResponses(value, responses)
			if err != nil {
				return nil, err
			}
			inlined[i] = resolved
		}
		return inlined, nil
	default:
		return node, nil
	}
}

// swapIn replaces dst with src, keeping the old dst until the new one is in
// place: removing it first would leave no tree at all if the rename then fails
// (a cross-device stage dir, a permission problem, an editor holding a file),
// which is worse than the half-populated tree staging exists to prevent.
func swapIn(src, dst string) error {
	// The backup lives under the gitignored artifacts/ dir, on the same
	// filesystem as both trees, so the renames cannot cross a device.
	backup := filepath.Join("artifacts", filepath.Base(dst)+".prev")
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	if err := os.Rename(dst, backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("move %s aside: %w", dst, err)
	}
	if err := os.Rename(src, dst); err != nil {
		if restore := os.Rename(backup, dst); restore != nil {
			return fmt.Errorf("swap %s into %s: %w (and %s was left at %s)", src, dst, err, dst, backup)
		}
		return fmt.Errorf("swap %s into %s: %w", src, dst, err)
	}
	return os.RemoveAll(backup)
}

// pathItemTags collects the union of tags across a path item's operations.
// Untagged operations fall into an "Other" package so nothing is silently lost.
func pathItemTags(pathItem map[string]any) []string {
	seen := map[string]bool{}
	var tags []string
	for _, method := range httpMethods {
		op, ok := pathItem[method].(map[string]any)
		if !ok {
			continue
		}
		opTags, _ := op["tags"].([]any)
		if len(opTags) == 0 {
			opTags = []any{"Other"}
		}
		for _, t := range opTags {
			tag, ok := t.(string)
			if !ok || seen[tag] {
				continue
			}
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	return tags
}

// packageName turns an OpenAPI tag into a Go package name: "Network Volumes" -> "networkvolumes".
func packageName(tag string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(tag) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func writeYAML(path string, doc any) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	// A close error on a file just written means the bytes may never have
	// reached disk, which would hand oapi-codegen a truncated spec: report it
	// unless the encode already failed with something more specific.
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", path, cerr)
		}
	}()
	enc := yaml.NewEncoder(f)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return enc.Close()
}
