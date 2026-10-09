package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/upgrade"
)

// upgradeUsage is the one form crew upgrade takes.
const upgradeUsage = "usage: crew upgrade [vX.Y.Z]"

// upgrader is what crew upgrade takes from the process, so tests can
// inject it.
type upgrader struct {
	// stamped is main.version, which crew's release stamps.
	stamped string
	// info is the binary's build info, nil when it has none.
	info *debug.BuildInfo
	// exe returns the running binary's path.
	exe          func() (string, error)
	goos, goarch string
	// apiEnv is the value of upgrade.APIEnv.
	apiEnv string
	http   *http.Client
	token  upgrade.TokenFunc
}

// runUpgrade runs crew upgrade with args, the arguments after "upgrade",
// and returns crew's exit code: 0 once crew is the target version, 1 when
// the download or the check failed, and 2 for a command line, a binary or
// a directory crew upgrade cannot use. The first Ctrl-C, SIGTERM or SIGHUP
// stops it before it replaces anything.
func runUpgrade(args []string, stdout, stderr io.Writer, u upgrader) int {
	tag, err := upgrade.ParseArgs(args)
	if err != nil {
		code := upgradeExit(stderr, err)
		if err.Error() != upgradeUsage {
			_, _ = fmt.Fprintln(stderr, "crew: "+upgradeUsage)
		}
		return code
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	return upgradeExit(stderr, u.upgrade(ctx, tag, stdout, stderr))
}

// upgrade installs the release of tag, the latest when it is
// upgrade.Latest, over the running crew. It checks, in order, the API
// override, the build, whether crew already is the version asked, and
// whether crew may write its own path, before it downloads the archive.
func (u upgrader) upgrade(ctx context.Context, tag string, stdout, stderr io.Writer) error {
	api, err := upgrade.ResolveAPI(u.apiEnv)
	if err != nil {
		return err
	}
	if notice := api.Notice(); notice != "" {
		_, _ = fmt.Fprintln(stderr, "crew: "+notice)
	}
	running, err := releaseVersion(upgrade.Classify(u.stamped, u.info), tag)
	if err != nil {
		return err
	}
	if tag == running {
		return already(stdout, running)
	}
	target, err := upgrade.Target(u.exe)
	if err != nil {
		return err
	}
	client := upgrade.NewClient(api, u.http, u.token)
	rel, err := client.Release(ctx, tag)
	if err != nil {
		return unchanged(err, target)
	}
	if rel.Tag == running {
		return already(stdout, running)
	}
	if err := upgrade.CheckWritable(target); err != nil {
		return err
	}
	binary, err := client.Binary(ctx, rel, u.goos, u.goarch)
	if err != nil {
		return unchanged(err, target)
	}
	if err := upgrade.Replace(ctx, target, binary); err != nil {
		return err
	}
	// crew is installed: a closed stdout does not undo that.
	_, _ = fmt.Fprintf(stdout, "crew: upgraded %s to %s at %s\n", running, rel.Tag, target)
	return nil
}

// unchanged returns err, saying that crew is unchanged at target when err
// is an interrupt, which otherwise would not say what the user's binary is.
func unchanged(err error, target string) error {
	if errors.Is(err, upgrade.ErrInterrupted) {
		return fmt.Errorf("%w; crew is unchanged at %s", err, target)
	}
	return err
}

// releaseVersion returns the version of build when it is a release build,
// and otherwise an EnvError saying what to run instead; tag is the version
// asked, upgrade.Latest for the latest.
func releaseVersion(build upgrade.Build, tag string) (string, error) {
	switch build.Kind {
	case upgrade.ReleaseBuild:
		return build.Version, nil
	case upgrade.GoInstallBuild:
		if tag == upgrade.Latest {
			tag = "latest"
		}
		return "", &upgrade.EnvError{Err: fmt.Errorf("this crew was built by go install, so crew upgrade leaves it; "+
			"upgrade it with: go install github.com/thatsnotmynameio/crew/cmd/crew@%s", tag)}
	default:
		return "", &upgrade.EnvError{Err: fmt.Errorf("this crew is a local build (%s), so crew upgrade leaves it; "+
			"crew upgrade replaces only a release, which the README's install command installs", build.Version)}
	}
}

// already says crew already is version.
func already(stdout io.Writer, version string) error {
	_, _ = fmt.Fprintf(stdout, "crew: already %s\n", version)
	return nil
}

// upgradeExit prints err, when there is one, and returns the exit code it
// means: 2 for an *upgrade.EnvError, 1 for any other error, and 0 for none.
func upgradeExit(stderr io.Writer, err error) int {
	if err == nil {
		return app.ExitClean
	}
	_, _ = fmt.Fprintf(stderr, "crew: %v\n", err)
	if _, ok := errors.AsType[*upgrade.EnvError](err); ok {
		return app.ExitConfig
	}
	return app.ExitFailure
}
