// Command release checks that the Release workflow
// (.github/workflows/release.yml) may publish the version VERSION names:
//
//	go run ./tools/release check -ref "$GITHUB_REF" -notes "$RUNNER_TEMP/notes.md" >> "$GITHUB_OUTPUT"
//
// check refuses unless the run was started on main, CHANGELOG.md has a
// section for the version, and GitHub has neither the version's tag nor a
// published release of it. A draft does not count: it is what the workflow's
// build job leaves for its publish job, and what a failed run leaves, which
// GoReleaser then finishes. On a pass, check prints the version and its tag
// as outputs and writes the section to the notes file, the release text.
// Each refusal or error prints one ::error:: line, which fails the step
// with its reason.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// mainRef is the only ref a release runs from.
const mainRef = "refs/heads/main"

// notesPerm is the mode of the notes file.
const notesPerm = 0o600

// Exit codes.
const (
	exitPass    = 0
	exitRefused = 1
	exitError   = 2
)

// refusal is a reason the release must not publish, as opposed to an error
// that kept check from deciding.
type refusal string

func (r refusal) Error() string { return string(r) }

// refusalf returns a refusal with a formatted reason.
func refusalf(format string, args ...any) error {
	return refusal(fmt.Sprintf(format, args...))
}

// checkFlags are the check subcommand's flags.
type checkFlags struct {
	ref       string
	version   string
	changelog string
	notes     string
}

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run is main without the process: it reads the Actions environment
// through getenv and returns the exit code.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	err := dispatch(args, getenv, stdout)
	if err == nil {
		return exitPass
	}
	_, _ = fmt.Fprintf(stderr, "::error::release: %v\n", err)
	if _, ok := errors.AsType[refusal](err); ok {
		return exitRefused
	}

	return exitError
}

// dispatch runs the subcommand args name.
func dispatch(args []string, getenv func(string) string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "check" {
		return errors.New("usage: release check [-ref ref] [-version path] [-changelog path] -notes path")
	}
	flags := flag.NewFlagSet("release check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var f checkFlags
	flags.StringVar(&f.ref, "ref", "", "the ref the run was started on, GITHUB_REF")
	flags.StringVar(&f.version, "version", "VERSION", "the file that names the version")
	flags.StringVar(&f.changelog, "changelog", "CHANGELOG.md", "the changelog")
	flags.StringVar(&f.notes, "notes", "", "the file to write the release notes to, outside the checkout")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("release check: %w", err)
	}

	return check(context.Background(), f, getenv, stdout)
}

// check refuses a release f's version must not have, and otherwise writes
// its notes and prints its outputs.
func check(ctx context.Context, f checkFlags, getenv func(string) string, stdout io.Writer) error {
	if f.ref != mainRef {
		return refusalf("only main releases: this run was started on %q", f.ref)
	}
	version, err := readFile(f.version)
	if err != nil {
		return err
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return fmt.Errorf("%s is empty", f.version)
	}
	changelog, err := readFile(f.changelog)
	if err != nil {
		return err
	}
	notes, err := section(changelog, version)
	if err != nil {
		return err
	}
	tag := "v" + version
	if err := unreleased(ctx, getenv, version, tag); err != nil {
		return err
	}
	if err := os.WriteFile(f.notes, []byte(notes+"\n"), notesPerm); err != nil {
		return fmt.Errorf("write the release notes: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "version=%s\ntag=%s\n", version, tag)

	return nil
}

// unreleased refuses a version GitHub already has a published release or
// a tag of.
func unreleased(ctx context.Context, getenv func(string) string, version, tag string) error {
	gh, err := newGitHub(getenv)
	if err != nil {
		return err
	}
	tagged, err := gh.exists(ctx, "git", "ref", "tags", tag)
	if err != nil {
		return err
	}
	published, err := gh.exists(ctx, "releases", "tags", tag)
	if err != nil {
		return err
	}
	if published {
		return refusalf("%s already has a release: %s is published", version, tag)
	}
	if tagged {
		return refusalf("the tag %s exists without a published release: delete it, or bump VERSION", tag)
	}

	return nil
}

// readFile returns the content of the file at path.
func readFile(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the path is one of the command's own flags
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	return string(data), nil
}
