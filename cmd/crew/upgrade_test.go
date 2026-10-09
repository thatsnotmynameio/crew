package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/upgrade"
)

// releases is a fake GitHub serving crew releases: each tag's archive for
// linux/amd64 holds a crew whose bytes are "crew <tag>".
type releases struct {
	t      *testing.T
	latest string
	tags   []string
	// tampered is a tag whose archive does not match its checksum.
	tampered string
	// onArchive, when set, answers an archive request instead.
	onArchive func(w http.ResponseWriter, req *http.Request)

	mu       sync.Mutex
	requests []string
}

func (r *releases) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.requests = append(r.requests, req.URL.Path)
	r.mu.Unlock()
	const prefix = "/repos/thatsnotmynameio/crew"
	path := req.URL.Path
	switch {
	case path == prefix:
		writeJSON(r.t, w, map[string]string{"full_name": "thatsnotmynameio/crew"})
	case path == prefix+"/releases/latest":
		r.release(w, r.latest)
	case strings.HasPrefix(path, prefix+"/releases/tags/"):
		r.release(w, strings.TrimPrefix(path, prefix+"/releases/tags/"))
	case strings.HasPrefix(path, prefix+"/releases/assets/"):
		r.asset(w, req, strings.TrimPrefix(path, prefix+"/releases/assets/"))
	default:
		http.NotFound(w, req)
	}
}

// requested returns the paths the fake was asked for.
func (r *releases) requested() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.requests...)
}

// downloads returns the asset paths the fake was asked for.
func (r *releases) downloads() []string {
	var paths []string
	for _, path := range r.requested() {
		if strings.Contains(path, "/assets/") {
			paths = append(paths, path)
		}
	}
	return paths
}

// release answers the release of tag.
func (r *releases) release(w http.ResponseWriter, tag string) {
	i := r.index(tag)
	if i < 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		return
	}
	writeJSON(r.t, w, map[string]any{
		"tag_name": tag, "draft": false, "prerelease": false,
		"assets": []map[string]any{
			{"id": i*10 + 1, "name": "crew_linux_amd64.tar.gz"},
			{"id": i*10 + 2, "name": "checksums.txt"},
		},
	})
}

// index returns the index of tag, -1 when the fake has no such release.
func (r *releases) index(tag string) int {
	for i, t := range r.tags {
		if t == tag {
			return i
		}
	}
	return -1
}

// asset answers the asset of id: odd ids are archives, even ones
// checksums.txt.
func (r *releases) asset(w http.ResponseWriter, req *http.Request, id string) {
	n, err := strconv.Atoi(id)
	if err != nil || n/10 >= len(r.tags) {
		http.NotFound(w, req)
		return
	}
	if n%10 == 1 && r.onArchive != nil {
		r.onArchive(w, req)
		return
	}
	tag := r.tags[n/10]
	archive := crewArchive(r.t, "crew "+tag)
	sum := sha256.Sum256(archive)
	if tag == r.tampered {
		archive = append(archive, 0)
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	if n%10 == 1 {
		_, _ = w.Write(archive)
		return
	}
	_, _ = fmt.Fprintf(w, "%s  crew_linux_amd64.tar.gz\n", hex.EncodeToString(sum[:]))
}

// writeJSON answers v as JSON.
func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encoding %v: %v", v, err)
	}
}

// crewArchive returns a release archive whose crew holds body.
func crewArchive(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// A fixed order, so every request for the archive gets the same bytes.
	for _, file := range [][2]string{{"crew", body}, {"LICENSE", "MIT"}} {
		name, content := file[0], file[1]
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Error(err)
		}
		_, _ = tw.Write([]byte(content))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// noLogin is a gh login lookup that finds none.
func noLogin(context.Context, upgrade.API) (string, string) {
	return "", "no gh login in this test"
}

// upgradeFixture is a crew in a temp dir, and a fake GitHub.
type upgradeFixture struct {
	exe      string
	releases *releases
	deps     upgrader
}

// newUpgrade returns a fixture whose crew is a build of kind at version,
// with the fake's releases tags and v0.5.0 the latest.
func newUpgrade(t *testing.T, kind upgrade.Kind, version string, tags ...string) *upgradeFixture {
	t.Helper()
	// Resolved: crew upgrade reports the install's real path, and on macOS
	// the temp directory sits behind the /var -> /private/var link.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "crew")
	if err := os.WriteFile(exe, []byte("crew "+version), 0o700); err != nil {
		t.Fatal(err)
	}
	rel := &releases{t: t, latest: "v0.5.0", tags: tags}
	srv := httptest.NewServer(rel)
	t.Cleanup(srv.Close)
	deps := upgrader{
		exe: func() (string, error) { return exe, nil }, goos: "linux", goarch: "amd64",
		apiEnv: srv.URL, http: srv.Client(), token: noLogin,
	}
	switch kind {
	case upgrade.ReleaseBuild:
		deps.stamped = version
	case upgrade.GoInstallBuild:
		deps.info = &debug.BuildInfo{Main: debug.Module{Version: version, Sum: "h1:abc="}}
	case upgrade.LocalBuild:
		deps.info = &debug.BuildInfo{Main: debug.Module{Version: version}}
	}
	return &upgradeFixture{exe: exe, releases: rel, deps: deps}
}

// run runs crew upgrade with args and returns its exit code and output.
func (f *upgradeFixture) run(args ...string) captured {
	var stdout, stderr bytes.Buffer
	code := runUpgrade(args, &stdout, &stderr, f.deps)
	return captured{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// holds fails the test unless the installed crew holds "crew <version>"
// and no temp file is left beside it.
func (f *upgradeFixture) holds(t *testing.T, version string) {
	t.Helper()
	data, err := os.ReadFile(f.exe)
	if err != nil || string(data) != "crew "+version {
		t.Errorf("the installed crew holds %q, %v, want crew %s", data, err, version)
	}
	entries, err := os.ReadDir(filepath.Dir(f.exe))
	if err != nil || len(entries) != 1 {
		t.Errorf("the install directory holds %v, %v, want only crew", entries, err)
	}
}

func TestUpgradeInstallsTheLatestRelease(t *testing.T) {
	// Covers AE1.
	f := newUpgrade(t, upgrade.ReleaseBuild, "v0.4.0", "v0.4.0", "v0.5.0")
	got := f.run()
	if got.code != app.ExitClean {
		t.Fatalf("code = %d, want %d (stderr %q)", got.code, app.ExitClean, got.stderr)
	}
	if want := "crew: upgraded v0.4.0 to v0.5.0 at " + f.exe + "\n"; got.stdout != want {
		t.Errorf("stdout = %q, want %q", got.stdout, want)
	}
	f.holds(t, "v0.5.0")
}

func TestUpgradeSaysWhenCrewIsAlreadyTheLatest(t *testing.T) {
	// Covers AE2.
	f := newUpgrade(t, upgrade.ReleaseBuild, "v0.5.0", "v0.5.0")
	got := f.run()
	if got.code != app.ExitClean || got.stdout != "crew: already v0.5.0\n" {
		t.Errorf("crew upgrade = %+v, want exit 0 saying already v0.5.0", got)
	}
	f.holds(t, "v0.5.0")
	if paths := f.releases.downloads(); len(paths) != 0 {
		t.Errorf("crew upgrade downloaded %q, want nothing", paths)
	}
}

func TestUpgradeInstallsAnOlderNamedReleaseWithoutReadingStdin(t *testing.T) {
	// Covers AE3.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = r.Close(), w.Close() })
	if _, err := w.WriteString("y\n"); err != nil {
		t.Fatal(err)
	}
	oldIn := os.Stdin
	t.Cleanup(func() { os.Stdin = oldIn })
	os.Stdin = r
	f := newUpgrade(t, upgrade.ReleaseBuild, "v0.5.0", "v0.4.0", "v0.5.0")
	if got := f.run("v0.4.0"); got.code != app.ExitClean {
		t.Fatalf("crew upgrade v0.4.0 = %+v, want exit 0", got)
	}
	f.holds(t, "v0.4.0")
	_ = w.Close()
	if left, err := io.ReadAll(r); err != nil || string(left) != "y\n" {
		t.Errorf("stdin still holds %q, %v, want the answer crew never read", left, err)
	}
}

func TestUpgradeToTheRunningVersionAsksNothingOfGitHub(t *testing.T) {
	f := newUpgrade(t, upgrade.ReleaseBuild, "v0.5.0", "v0.5.0")
	if got := f.run("v0.5.0"); got.code != app.ExitClean || got.stdout != "crew: already v0.5.0\n" {
		t.Errorf("crew upgrade v0.5.0 = %+v, want exit 0 saying already v0.5.0", got)
	}
	if paths := f.releases.requested(); len(paths) != 0 {
		t.Errorf("requests = %q, want none", paths)
	}
}

func TestUpgradeTellsAGoInstallBuildTheCommand(t *testing.T) {
	// Covers AE7.
	f := newUpgrade(t, upgrade.GoInstallBuild, "v0.4.0", "v0.5.0")
	for args, want := range map[string]string{"": "@latest", "v0.4.0": "@v0.4.0"} {
		got := f.run(strings.Fields(args)...)
		if got.code != app.ExitConfig ||
			!strings.Contains(got.stderr, "go install github.com/thatsnotmynameio/crew/cmd/crew"+want+"\n") {
			t.Errorf("crew upgrade %s = %+v, want exit 2 with the go install command for %s", args, got, want)
		}
	}
	f.holds(t, "v0.4.0")
	if paths := f.releases.requested(); len(paths) != 0 {
		t.Errorf("requests = %q, want none", paths)
	}
}

func TestUpgradeRefusesALocalBuild(t *testing.T) {
	// Covers AE8.
	const pseudo = "v0.1.1-0.20261007164002-5483064250f9"
	f := newUpgrade(t, upgrade.LocalBuild, pseudo, "v0.5.0")
	got := f.run()
	if got.code != app.ExitConfig || !strings.Contains(got.stderr, "local build ("+pseudo+")") {
		t.Errorf("crew upgrade = %+v, want exit 2 saying it is a local build of %s", got, pseudo)
	}
	f.holds(t, pseudo)
	if paths := f.releases.requested(); len(paths) != 0 {
		t.Errorf("requests = %q, want none", paths)
	}
}

func TestUpgradeInADirectoryCrewCannotWriteChangesNothing(t *testing.T) {
	// Covers AE6.
	if os.Geteuid() == 0 {
		t.Skip("root writes any directory")
	}
	f := newUpgrade(t, upgrade.ReleaseBuild, "v0.4.0", "v0.4.0", "v0.5.0")
	dir := filepath.Dir(f.exe)
	if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // G302: a test directory made read-only
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // G302: the test's own directory
	got := f.run()
	if got.code != app.ExitConfig || !strings.Contains(got.stderr, "crew cannot replace "+f.exe) ||
		!strings.Contains(got.stderr, "~/.local/bin") {
		t.Errorf("crew upgrade = %+v, want exit 2 naming %s and pointing to ~/.local/bin", got, f.exe)
	}
	f.holds(t, "v0.4.0")
	if paths := f.releases.downloads(); len(paths) != 0 {
		t.Errorf("crew upgrade downloaded %q, want nothing", paths)
	}
}

func TestUpgradeFailuresLeaveTheBinaryAndExitOne(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "checksum mismatch", want: "checksum mismatch"},
		{name: "missing release", args: []string{"v9.9.9"}, want: "release v9.9.9 does not exist"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newUpgrade(t, upgrade.ReleaseBuild, "v0.4.0", "v0.4.0", "v0.5.0")
			f.releases.tampered = "v0.5.0"
			got := f.run(tt.args...)
			if got.code != app.ExitFailure || !strings.Contains(got.stderr, tt.want) {
				t.Errorf("crew upgrade = %+v, want exit 1 saying %q", got, tt.want)
			}
			f.holds(t, "v0.4.0")
		})
	}
}

func TestUpgradeInterruptedLeavesTheBinaryAndExitsOne(t *testing.T) {
	f := newUpgrade(t, upgrade.ReleaseBuild, "v0.4.0", "v0.4.0", "v0.5.0")
	// The archive's download is where the user presses Ctrl-C.
	f.releases.onArchive = func(_ http.ResponseWriter, req *http.Request) {
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		select {
		case <-req.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}
	got := f.run()
	if got.code != app.ExitFailure || !strings.Contains(got.stderr, "the upgrade was interrupted") ||
		!strings.Contains(got.stderr, "crew is unchanged at "+f.exe) {
		t.Errorf("crew upgrade = %+v, want exit 1 saying crew is unchanged at %s", got, f.exe)
	}
	if strings.Contains(got.stderr, "could not reach GitHub") {
		t.Errorf("stderr = %q, want no blame on GitHub", got.stderr)
	}
	f.holds(t, "v0.4.0")
}

func TestUpgradeRefusesAMalformedCommandLineBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{{"v1", "extra"}, {"v1.2"}, {"latest"}, {"--help"}, {"-h"}} {
		f := newUpgrade(t, upgrade.ReleaseBuild, "v0.4.0", "v0.5.0")
		got := f.run(args...)
		if got.code != app.ExitConfig || !strings.HasSuffix(got.stderr, "crew: usage: crew upgrade [vX.Y.Z]\n") {
			t.Errorf("crew upgrade %q = %+v, want exit 2 ending with the usage line", args, got)
		}
		if strings.Count(got.stderr, "usage:") != 1 {
			t.Errorf("crew upgrade %q printed %q, want the usage line once", args, got.stderr)
		}
		if paths := f.releases.requested(); len(paths) != 0 {
			t.Errorf("crew upgrade %q requested %q, want nothing", args, paths)
		}
	}
}

func TestUpgradeRefusesAnOverrideThatIsNotLoopback(t *testing.T) {
	f := newUpgrade(t, upgrade.ReleaseBuild, "v0.4.0", "v0.5.0")
	f.deps.apiEnv = "https://example.com"
	if got := f.run(); got.code != app.ExitConfig || !strings.Contains(got.stderr, upgrade.APIEnv) {
		t.Errorf("crew upgrade = %+v, want exit 2 naming %s", got, upgrade.APIEnv)
	}
}

func TestUpgradeNamesTheOverrideOnStderr(t *testing.T) {
	f := newUpgrade(t, upgrade.ReleaseBuild, "v0.5.0", "v0.5.0")
	want := "crew: finding crew's releases at " + f.deps.apiEnv + ", set by " + upgrade.APIEnv + "\n"
	if got := f.run(); got.stderr != want {
		t.Errorf("stderr = %q, want %q", got.stderr, want)
	}
}

func TestHelpListsUpgrade(t *testing.T) {
	got := runCaptured(t, "--help")
	if got.code != app.ExitClean || !strings.Contains(got.stderr, "crew upgrade [vX.Y.Z]") {
		t.Errorf("crew --help = %+v, want crew upgrade listed", got)
	}
}

func TestRunDispatchesUpgrade(t *testing.T) {
	// The test binary carries no stamped version, so it is a local build.
	t.Setenv(upgrade.APIEnv, "")
	got := runCaptured(t, "upgrade")
	if got.code != app.ExitConfig || !strings.Contains(got.stderr, "local build") {
		t.Errorf("crew upgrade = %+v, want exit 2 saying it is a local build", got)
	}
}

func TestUpgradeExitsZeroWhenStdoutIsClosedAfterInstalling(t *testing.T) {
	if os.Getenv("CREW_TEST_UPGRADE_CLOSED") == "1" {
		deps := upgrader{
			stamped: "v0.4.0", exe: func() (string, error) { return os.Getenv("CREW_TEST_UPGRADE_EXE"), nil },
			goos: "linux", goarch: "amd64", apiEnv: os.Getenv("CREW_TEST_UPGRADE_API"), http: &http.Client{},
			token: noLogin,
		}
		// run ignores SIGPIPE before it dispatches crew upgrade.
		signal.Ignore(syscall.SIGPIPE)
		os.Exit(runUpgrade(nil, os.Stdout, os.Stderr, deps))
	}
	t.Cleanup(func() { signal.Reset() })
	f := newUpgrade(t, upgrade.ReleaseBuild, "v0.4.0", "v0.4.0", "v0.5.0")
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	defer func() { _ = w.Close() }()
	var stderr bytes.Buffer
	//nolint:gosec // G702: os.Args[0] is this test binary, re-run as crew upgrade with a closed stdout
	cmd := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=^TestUpgradeExitsZeroWhenStdoutIsClosedAfterInstalling$")
	cmd.Env = append(os.Environ(), "CREW_TEST_UPGRADE_CLOSED=1",
		"CREW_TEST_UPGRADE_EXE="+f.exe, "CREW_TEST_UPGRADE_API="+f.deps.apiEnv)
	cmd.Stdout, cmd.Stderr = w, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("crew upgrade with a closed stdout: %v, want exit 0 (stderr %q)", err, stderr.String())
	}
	f.holds(t, "v0.5.0")
}
