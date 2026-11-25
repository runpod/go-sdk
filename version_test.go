package runpod

import (
	"runtime/debug"
	"testing"
)

func TestVersionFromBuildInfo(t *testing.T) {
	const path = "github.com/runpod/go-sdk/v2"
	tests := []struct {
		name string
		deps []*debug.Module
		want string
	}{
		{"dependency", []*debug.Module{{Path: path, Version: "v2.3.0"}}, "2.3.0"},
		{"replaced by a local path", []*debug.Module{{Path: path, Version: "v2.3.0", Replace: &debug.Module{Path: "../go-sdk"}}}, Version + "-dev"},
		{"not a dependency", nil, Version + "-dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionFromBuildInfo(&debug.BuildInfo{Deps: tt.deps}); got != tt.want {
				t.Errorf("versionFromBuildInfo() = %q, want %q", got, tt.want)
			}
		})
	}
}
