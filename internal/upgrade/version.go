package upgrade

import (
	"errors"
	"fmt"
	"regexp"
	"runtime/debug"
)

// EnvError is an error in crew upgrade's environment, one the command
// cannot change: a command line it does not take or a binary that is not
// a release build. crew exits 2 on it.
type EnvError struct {
	// Err is what went wrong.
	Err error
}

// Error returns the message of the wrapped error.
func (e *EnvError) Error() string { return e.Err.Error() }

// Unwrap returns the wrapped error.
func (e *EnvError) Unwrap() error { return e.Err }

// envErrorf formats an error, as fmt.Errorf does, and marks it an EnvError.
func envErrorf(format string, args ...any) error {
	return &EnvError{Err: fmt.Errorf(format, args...)}
}

// Kind is how a crew binary was built.
type Kind int

const (
	// LocalBuild is any build crew upgrade does not replace other than a
	// go install one, such as go build in a checkout.
	LocalBuild Kind = iota
	// ReleaseBuild is a build of crew's release, which stamps main.version.
	ReleaseBuild
	// GoInstallBuild is a build by go install of a module version.
	GoInstallBuild
)

// Build is how the running crew was built, and its version.
type Build struct {
	Kind    Kind
	Version string
}

// Classify returns how a crew was built from stamped, the main.version
// its release stamps, and info, its build info (nil when it has none). A
// stamped version is a release build. Without one, a main module with a
// checksum is a go install build: go build in a checkout records none,
// even at a tag, where it records the same version a release has. Anything
// else is a local build, whose version is "dev" when Go recorded none.
func Classify(stamped string, info *debug.BuildInfo) Build {
	if stamped != "" {
		return Build{Kind: ReleaseBuild, Version: stamped}
	}
	if info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return Build{Kind: LocalBuild, Version: "dev"}
	}
	if info.Main.Sum != "" {
		return Build{Kind: GoInstallBuild, Version: info.Main.Version}
	}
	return Build{Kind: LocalBuild, Version: info.Main.Version}
}

// Latest is the version ParseArgs returns when crew upgrade names none:
// the latest release.
const Latest = ""

// usage is the one form crew upgrade takes.
const usage = "usage: crew upgrade [vX.Y.Z]"

// releaseVersion is a release's version, MAJOR.MINOR.PATCH with an
// optional v and no leading zeros, as CI's version check holds VERSION to.
var releaseVersion = regexp.MustCompile(`^v?((?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*))$`)

// ParseArgs returns the release named by args, the arguments after "upgrade":
// Latest for none, and vX.Y.Z for one release version written vX.Y.Z or
// X.Y.Z. It returns an EnvError naming the argument when it is not a
// release version, and one whose text is the usage line for -h, --help or
// more than one argument.
func ParseArgs(args []string) (string, error) {
	switch {
	case len(args) == 0:
		return Latest, nil
	case len(args) > 1 || args[0] == "-h" || args[0] == "--help":
		return "", &EnvError{Err: errors.New(usage)}
	}
	m := releaseVersion.FindStringSubmatch(args[0])
	if m == nil {
		return "", envErrorf("%q is not a release version; crew upgrade takes vX.Y.Z", args[0])
	}
	return "v" + m[1], nil
}
