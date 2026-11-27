package runpod

import (
	"runtime/debug"
	"strings"
)

// BuildVersion reports the version of this module linked into the running
// binary, read from Go's build information, without the leading "v". When Go
// recorded none (a local replace, this module's own tests) it is Version with a
// "-dev" suffix. The User-Agent header carries it.
func BuildVersion() string {
	info, _ := debug.ReadBuildInfo()
	return versionFromBuildInfo(info)
}

func versionFromBuildInfo(info *debug.BuildInfo) string {
	if info != nil {
		for _, dep := range info.Deps {
			if dep.Path != "github.com/runpod/go-sdk/v2" {
				continue
			}
			if dep.Replace != nil {
				dep = dep.Replace
			}
			// A replace by a local path records "(devel)", not a version.
			if dep.Version != "" && dep.Version != "(devel)" {
				return strings.TrimPrefix(dep.Version, "v")
			}
		}
	}
	return Version + "-dev"
}
