package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestFromBuildInfo(t *testing.T) {
	bi := &debug.BuildInfo{
		GoVersion: "go1.27.4",
		Main:      debug.Module{Version: "v1.4.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc123"},
			{Key: "vcs.time", Value: "2026-11-26T09:00:00Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}

	tests := []struct {
		name     string
		override string
		want     string
	}{
		{"embedded version", "", "v1.4.0 (go1.27.4, commit abc123-dirty, committed 2026-11-26T09:00:00Z)"},
		{"ldflags override", "v9.9.9", "v9.9.9 (go1.27.4, commit abc123-dirty, committed 2026-11-26T09:00:00Z)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fromBuildInfo(bi, tt.override).String(); got != tt.want {
				t.Errorf("String() =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

func TestFromBuildInfoNoVCS(t *testing.T) {
	got := fromBuildInfo(&debug.BuildInfo{GoVersion: "go1.27.4"}, "")
	if got.Version != "(devel)" || got.Revision != "" {
		t.Errorf("fromBuildInfo() = %+v, want devel version without revision", got)
	}
}

func TestRead(t *testing.T) {
	if Read().GoVersion == "" {
		t.Error("Read().GoVersion is empty; test binaries always embed build info")
	}
}
