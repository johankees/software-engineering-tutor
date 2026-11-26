// Package buildinfo reports which version of the code is running.
//
// The Go toolchain embeds the module version and VCS details (commit, time,
// dirty flag) in every binary it builds inside a repository. Builds without
// a repository, such as a Docker build without .git, can set the version
// with -ldflags "-X github.com/acme/orders/internal/buildinfo.version=v1.2.3".
package buildinfo

import (
	"fmt"
	"runtime/debug"
)

// version is set at link time with -ldflags -X. It wins over the version
// the toolchain embeds.
var version string

// Info describes the running binary.
type Info struct {
	Version   string
	Revision  string
	Time      string
	Modified  bool
	GoVersion string
}

// String formats Info as one line, for --version output and logs.
func (i Info) String() string {
	s := fmt.Sprintf("%s (%s", i.Version, i.GoVersion)
	if i.Revision != "" {
		s += ", commit " + i.Revision
		if i.Modified {
			s += "-dirty"
		}
	}
	if i.Time != "" {
		s += ", committed " + i.Time
	}
	return s + ")"
}

// Read returns the build information of the running binary.
func Read() Info {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return Info{Version: orDefault(version, "unknown")}
	}
	return fromBuildInfo(bi, version)
}

func fromBuildInfo(bi *debug.BuildInfo, override string) Info {
	info := Info{
		Version:   orDefault(override, bi.Main.Version),
		GoVersion: bi.GoVersion,
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Revision = s.Value
		case "vcs.time":
			info.Time = s.Value
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
	info.Version = orDefault(info.Version, "(devel)")
	return info
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
