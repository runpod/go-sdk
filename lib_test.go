package runpod

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The v2 tag freezes the generated type layout, and the layout it freezes is
// one type per schema: a schema several API areas share is declared once in
// lib/types and imported, not copied into every area package. Unifying copies
// afterwards is a breaking change that would need a /v3, so the invariant is
// asserted against the generated source rather than left to a reading of
// generator/main.go.

// clientPlumbing is what oapi-codegen emits once per generated client package.
// These names are a package's own transport machinery and carry no meaning
// across areas, so a declaration per package is correct.
var clientPlumbing = []string{
	"Client",
	"ClientInterface",
	"ClientOption",
	"ClientWithResponses",
	"ClientWithResponsesInterface",
	"HttpRequestDoer",
	"RequestEditorFn",
}

func TestNoTypeIsDeclaredInTwoPackages(t *testing.T) {
	packages := generatedPackages(t)

	declaredIn := map[string][]string{}
	for pkg, generated := range packages {
		for _, name := range generated.types {
			declaredIn[name] = append(declaredIn[name], pkg)
		}
	}

	for name, pkgs := range declaredIn {
		if len(pkgs) < 2 || slices.Contains(clientPlumbing, name) {
			continue
		}
		if isSharedOperationWrapper(name, pkgs, packages) {
			continue
		}
		sort.Strings(pkgs)
		t.Errorf("%s is declared in %d packages (%s): a shared schema belongs in lib/types",
			name, len(pkgs), strings.Join(pkgs, ", "))
	}
}

func TestLibTypesHoldsTheSchemasTheAreasShare(t *testing.T) {
	// The schemas the per-tag split used to copy, most-copied first: nine
	// ErrorResponse declarations meant a consumer could not write one error
	// check across areas, and two Pod types meant a cluster's pods could not be
	// passed to anything in the pods package.
	shared := []string{
		"ErrorResponse", "UnauthorizedError", "ContainerConfig", "BaseContainerConfig",
		"PersistentMount", "CpuConfig", "Pod", "PodStatus", "PodAction", "Mounts",
		"Template", "VolumeType",
	}

	packages := generatedPackages(t)
	types, ok := packages["types"]
	if !ok {
		t.Fatal("lib/types was not generated")
	}
	for _, name := range shared {
		if !slices.Contains(types.types, name) {
			t.Errorf("lib/types does not declare %s", name)
		}
		for pkg, generated := range packages {
			if pkg != "types" && slices.Contains(generated.types, name) {
				t.Errorf("lib/%s declares its own %s", pkg, name)
			}
		}
	}
}

// isSharedOperationWrapper reports whether name is one of the wrappers
// oapi-codegen generates per operation — <Operation>Params, <Operation>Result,
// <Operation>JSONRequestBody and the response-header structs — for an operation
// every one of pkgs implements. A spec that tags one operation with two areas
// generates it into both packages, and those wrappers are per-package by
// construction; a schema name never is.
func isSharedOperationWrapper(name string, pkgs []string, packages map[string]generatedPackage) bool {
	for _, operation := range packages[pkgs[0]].operations {
		if operation == name || !strings.HasPrefix(name, operation) {
			continue
		}
		inEvery := true
		for _, pkg := range pkgs[1:] {
			if !slices.Contains(packages[pkg].operations, operation) {
				inEvery = false
				break
			}
		}
		if inEvery {
			return true
		}
	}
	return false
}

// generatedPackage is what one package under lib/ declares: its top-level type
// names, and the operations its response client implements.
type generatedPackage struct {
	types      []string
	operations []string
}

// generatedPackages reads those declarations out of the generated source, which
// is the only place the type layout is written down.
func generatedPackages(t *testing.T) map[string]generatedPackage {
	t.Helper()

	entries, err := os.ReadDir("lib")
	if err != nil {
		t.Fatal(err)
	}
	packages := map[string]generatedPackage{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		files, err := filepath.Glob(filepath.Join("lib", entry.Name(), "*.gen.go"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) == 0 {
			t.Fatalf("lib/%s holds no generated file", entry.Name())
		}

		var generated generatedPackage
		for _, file := range files {
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range parsed.Decls {
				switch decl := decl.(type) {
				case *ast.GenDecl:
					if decl.Tok != token.TYPE {
						continue
					}
					for _, spec := range decl.Specs {
						if spec, ok := spec.(*ast.TypeSpec); ok {
							generated.types = append(generated.types, spec.Name.Name)
						}
					}
				case *ast.FuncDecl:
					if operation, ok := clientOperation(decl); ok {
						generated.operations = append(generated.operations, operation)
					}
				}
			}
		}
		packages[entry.Name()] = generated
	}
	if len(packages) == 0 {
		t.Fatal("lib/ holds no generated package")
	}
	return packages
}

// clientOperation returns the operation a *ClientWithResponses method serves.
func clientOperation(decl *ast.FuncDecl) (string, bool) {
	if decl.Recv == nil || len(decl.Recv.List) != 1 {
		return "", false
	}
	pointer, ok := decl.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return "", false
	}
	receiver, ok := pointer.X.(*ast.Ident)
	if !ok || receiver.Name != "ClientWithResponses" {
		return "", false
	}
	return strings.CutSuffix(decl.Name.Name, "WithResponse")
}
