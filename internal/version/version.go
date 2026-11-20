// Package version reports the mexaroot version. It is one variable so the
// daemon banner, the client user agent and `-version` all agree.
//
// Three cases, in order:
//  1. release/snapshot builds: goreleaser injects the tag via
//     -X ...internal/version.Version=vX.Y.Z
//  2. `go install ...@vX.Y.Z` and `go run ...@vX.Y.Z`: Version stays "dev"
//     (nobody injects ldflags), but the module's build info records the
//     version, so VersionString() reports that
//  3. plain `go build` from a checkout: both are "dev", possibly with the
//     VCS revision if the build happened inside a git worktree
package version

import (
	"runtime/debug"
	"strings"
)

var Version = "dev"

// VersionString returns the best-known version: the injected one when set,
// otherwise the module version from build info, otherwise "dev" (plus the
// commit when the build tool embedded one).
func VersionString() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
		if rev := vcsRevision(info); rev != "" {
			return "dev-" + rev
		}
	}
	return Version
}

func vcsRevision(info *debug.BuildInfo) string {
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			rev := s.Value
			if len(rev) > 12 {
				rev = rev[:12]
			}
			return strings.TrimSpace(rev)
		}
	}
	return ""
}
