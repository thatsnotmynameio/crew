package upgrade

import (
	"errors"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
)

// module is build info whose main module has version and sum.
func module(version, sum string) *debug.BuildInfo {
	return &debug.BuildInfo{Main: debug.Module{Path: "github.com/thatsnotmynameio/crew", Version: version, Sum: sum}}
}

func TestClassifyTellsReleaseGoInstallAndLocalBuildsApart(t *testing.T) {
	const pseudo = "v0.1.1-0.20261007164002-5483064250f9"
	tests := []struct {
		name    string
		stamped string
		info    *debug.BuildInfo
		want    Build
	}{
		{name: "stamped release", stamped: "v0.4.0", info: module("v0.4.0", ""),
			want: Build{Kind: ReleaseBuild, Version: "v0.4.0"}},
		{name: "stamped over a go install", stamped: "v0.4.0", info: module("v0.3.0", "h1:abc="),
			want: Build{Kind: ReleaseBuild, Version: "v0.4.0"}},
		{name: "stamped without build info", stamped: "v0.4.0", want: Build{Kind: ReleaseBuild, Version: "v0.4.0"}},
		{name: "go install of a tag", info: module("v0.4.0", "h1:abc="),
			want: Build{Kind: GoInstallBuild, Version: "v0.4.0"}},
		{name: "go build at a tag", info: module("v0.4.0", ""), want: Build{Kind: LocalBuild, Version: "v0.4.0"}},
		{name: "go build off a tag", info: module(pseudo, ""), want: Build{Kind: LocalBuild, Version: pseudo}},
		{name: "dirty go build", info: module("v0.4.0+dirty", ""), want: Build{Kind: LocalBuild, Version: "v0.4.0+dirty"}},
		{name: "devel build", info: module("(devel)", ""), want: Build{Kind: LocalBuild, Version: "dev"}},
		{name: "no module version", info: module("", ""), want: Build{Kind: LocalBuild, Version: "dev"}},
		{name: "no build info", want: Build{Kind: LocalBuild, Version: "dev"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.stamped, tt.info); got != tt.want {
				t.Errorf("Classify(%q, %v) = %+v, want %+v", tt.stamped, tt.info, got, tt.want)
			}
		})
	}
}

func TestParseArgsNormalisesAReleaseVersion(t *testing.T) {
	for arg, want := range map[string]string{
		"v1.2.3":   "v1.2.3",
		"1.2.3":    "v1.2.3",
		"v0.10.0":  "v0.10.0",
		"10.20.30": "v10.20.30",
	} {
		got, err := ParseArgs([]string{arg})
		if err != nil || got != want {
			t.Errorf("ParseArgs([%q]) = %q, %v, want %q", arg, got, err, want)
		}
	}
}

func TestParseArgsWithoutAnArgumentMeansTheLatestRelease(t *testing.T) {
	for _, args := range [][]string{nil, {}} {
		got, err := ParseArgs(args)
		if err != nil || got != Latest {
			t.Errorf("ParseArgs(%q) = %q, %v, want %q", args, got, err, Latest)
		}
	}
}

// envError returns err as an *EnvError, failing the test when it is not one.
func envError(t *testing.T, args []string, err error) *EnvError {
	t.Helper()
	envErr, ok := errors.AsType[*EnvError](err)
	if !ok {
		t.Fatalf("ParseArgs(%q) = %v, want an *EnvError", args, err)
	}
	return envErr
}

func TestParseArgsRefusesAnythingButMajorMinorPatch(t *testing.T) {
	for _, arg := range []string{
		"", "v1.2", "1.2.3.4", "v01.2.3", "v1.02.3", "latest", "v1.2.3-rc1", "V1.2.3", "v1.2.3\x00", "-v",
	} {
		args := []string{arg}
		_, err := ParseArgs(args)
		got := envError(t, args, err).Error()
		for _, want := range []string{strconv.Quote(arg), "not a release version", "vX.Y.Z"} {
			if !strings.Contains(got, want) {
				t.Errorf("ParseArgs(%q) = %q, want it to contain %q", args, got, want)
			}
		}
	}
}

func TestParseArgsAnswersHelpWithTheUsageLine(t *testing.T) {
	for _, args := range [][]string{{"-h"}, {"--help"}, {"v1.2.3", "v1.2.4"}} {
		_, err := ParseArgs(args)
		if got := envError(t, args, err).Error(); got != "usage: crew upgrade [vX.Y.Z]" {
			t.Errorf("ParseArgs(%q) = %q, want the usage line", args, got)
		}
	}
}
