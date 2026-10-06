// Command acceptance builds crew's binary with the release config and runs the
// acceptance suite against it, in one step:
//
//	go -C acceptance run ./cmd/acceptance [go test flags]
//
// It builds a GoReleaser snapshot of crew for this machine into a temporary
// directory, prints the binary's path and its --version, then runs
// go test -race -count=1 over the acceptance module with CREW_BIN set to the
// binary and the given flags (such as -run or -v) passed through. It exits
// with the tests' status; a failed build exits before any test runs. When a
// test fails it keeps the temporary directory and prints where the tests'
// artifacts are; otherwise it removes it.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// goreleaser is the pinned GoReleaser the release workflow runs, so the
// binary under test is built the way a release builds it. Dependabot does
// not bump it: keep it at the version release.yml and ci.yml pin.
const goreleaser = "github.com/goreleaser/goreleaser/v2@v2.18.2"

// artifactsPerm is the mode of the directory the tests save artifacts in.
const artifactsPerm = 0o750

func main() {
	os.Exit(run(os.Args[1:]))
}

// run builds the binary, runs the suite with extra as further go test flags,
// and returns the exit code.
func run(extra []string) int {
	moduleDir, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "acceptance:", err)
		return 1
	}

	tmp, err := os.MkdirTemp("", "crew-acceptance-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "acceptance:", err)
		return 1
	}

	code, keep := buildAndTest(moduleDir, tmp, extra)
	if !keep {
		_ = os.RemoveAll(tmp)
	}

	return code
}

// buildAndTest builds crew from the repository that holds moduleDir into tmp,
// prints its version, and runs the suite in moduleDir. It returns the exit
// code, and whether to keep tmp: only when the suite failed, for its
// artifacts.
func buildAndTest(moduleDir, tmp string, extra []string) (int, bool) {
	bin := filepath.Join(tmp, "crew")
	if err := build(filepath.Dir(moduleDir), bin); err != nil {
		fmt.Fprintln(os.Stderr, "acceptance: building crew:", err)
		return exitCode(err), false
	}

	fmt.Fprintln(os.Stdout, "crew binary:", bin)

	if err := command(moduleDir, bin, "--version").Run(); err != nil {
		fmt.Fprintln(os.Stderr, "acceptance: crew --version:", err)
		return exitCode(err), false
	}

	// go test -outputdir needs the directory to exist.
	artifacts := filepath.Join(tmp, "artifacts")
	if err := os.Mkdir(artifacts, artifactsPerm); err != nil {
		fmt.Fprintln(os.Stderr, "acceptance:", err)
		return 1, false
	}

	if err := suite(moduleDir, bin, artifacts, extra); err != nil {
		fmt.Fprintf(os.Stderr, "acceptance: %v; the tests' artifacts are in %s\n", err, artifacts)
		return exitCode(err), true
	}

	return 0, false
}

// moduleRoot returns the directory of the acceptance module, from the go.mod
// the go command finds for the working directory (go -C acceptance sets it).
func moduleRoot() (string, error) {
	out, err := exec.CommandContext(context.Background(), "go", "env", "GOMOD").Output()
	if err != nil {
		return "", fmt.Errorf("go env GOMOD: %w", err)
	}

	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		return "", errors.New("not in the acceptance module: run go -C acceptance run ./cmd/acceptance")
	}

	return filepath.Dir(gomod), nil
}

// build runs the pinned GoReleaser snapshot build of crew for this machine
// in root and copies the binary to bin.
func build(root, bin string) error {
	err := command(root, "go", "run", goreleaser,
		"build", "--snapshot", "--single-target", "--clean", "--output", bin).Run()
	if err != nil {
		return fmt.Errorf("goreleaser build: %w", err)
	}

	return nil
}

// suite runs the acceptance module's tests in moduleDir against bin, saving
// the tests' artifacts in artifacts.
func suite(moduleDir, bin, artifacts string, extra []string) error {
	args := append([]string{
		"test", "-race", "-count=1", "-artifacts", "-outputdir", artifacts, "./...",
	}, extra...)

	cmd := command(moduleDir, "go", args...)
	cmd.Env = append(os.Environ(), "CREW_BIN="+bin)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go test: %w", err)
	}

	return nil
}

// command returns name with args to run in dir, with this process's standard
// streams.
func command(dir, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // G204: go or the crew just built
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd
}

// exitCode returns the exit status of a child that ran and failed, and 1 for
// a child that could not start.
func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() > 0 {
		return exit.ExitCode()
	}

	return 1
}
