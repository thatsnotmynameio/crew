package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testRepo  = "thatsnotmynameio/crew"
	testToken = "test-token"
	tagPath   = "/repos/thatsnotmynameio/crew/git/ref/tags/v0.2.1"
	relPath   = "/repos/thatsnotmynameio/crew/releases/tags/v0.2.1"
	changelog = "# Changelog\n\n## 0.2.1\n\nWhat changed for you.\n\n## 0.2.0\n\nOlder.\n"
)

// fakeGitHub is a test GitHub that answers each path with its status, 404 for
// any other, and records the paths it was asked for.
type fakeGitHub struct {
	status   map[string]int
	requests []string
}

func (g *fakeGitHub) serve(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.requests = append(g.requests, r.URL.EscapedPath())
		checkHeaders(t, r)
		status, ok := g.status[r.URL.EscapedPath()]
		if !ok {
			status = http.StatusNotFound
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"message":"from the test"}`))
	}))
	t.Cleanup(srv.Close)

	return srv.URL
}

// checkHeaders checks that r is a GET with the headers GitHub's API asks for
// and the token.
func checkHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Method != http.MethodGet {
		t.Errorf("method %s, want GET", r.Method)
	}
	for header, want := range map[string]string{
		"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28",
		"Authorization": "Bearer " + testToken,
	} {
		if got := r.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

// setup is one release check's files and environment.
type setup struct {
	ref       string
	version   string
	changelog string
	env       map[string]string
	dir       string
}

// newSetup returns a check of 0.2.1 from main, with a changelog section for
// it, against GitHub at api.
func newSetup(t *testing.T, api string) *setup {
	t.Helper()

	return &setup{
		ref: "refs/heads/main", version: "0.2.1\n", changelog: changelog, dir: t.TempDir(),
		env: map[string]string{"GITHUB_API_URL": api, "GITHUB_REPOSITORY": testRepo, "GITHUB_TOKEN": testToken},
	}
}

func (s *setup) path(name string) string { return filepath.Join(s.dir, name) }

// run writes the files and runs the check, returning the exit code, stdout
// and stderr.
func (s *setup) run(t *testing.T, extra ...string) (int, string, string) {
	t.Helper()
	write(t, s.path("VERSION"), s.version)
	write(t, s.path("CHANGELOG.md"), s.changelog)
	args := append([]string{
		"check", "-ref", s.ref, "-version", s.path("VERSION"), "-changelog", s.path("CHANGELOG.md"),
		"-notes", s.path("notes.md"),
	}, extra...)
	var stdout, stderr bytes.Buffer
	code := run(args, func(key string) string { return s.env[key] }, &stdout, &stderr)

	return code, stdout.String(), stderr.String()
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// wantRefusal checks that the check failed with code and one ::error:: line
// on stderr that contains each of says, and printed nothing on stdout.
func wantRefusal(t *testing.T, code int, stdout, stderr string, wantCode int, says ...string) {
	t.Helper()
	if code != wantCode || stdout != "" || !strings.HasPrefix(stderr, "::error::") {
		t.Fatalf("code %d, stdout %q, stderr %q; want %d, no output and an ::error:: line", code, stdout, stderr, wantCode)
	}
	for _, s := range says {
		if !strings.Contains(stderr, s) {
			t.Errorf("stderr %q does not say %q", stderr, s)
		}
	}
}

func TestCheckPassesAndWritesTheNotes(t *testing.T) {
	gh := &fakeGitHub{}
	s := newSetup(t, gh.serve(t))
	code, stdout, stderr := s.run(t)
	if code != 0 || stdout != "version=0.2.1\ntag=v0.2.1\n" || stderr != "" {
		t.Fatalf("code %d, stdout %q, stderr %q; want 0 and the version and tag", code, stdout, stderr)
	}
	notes, err := os.ReadFile(s.path("notes.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(notes) != "What changed for you.\n" {
		t.Fatalf("notes %q, want the section's text", notes)
	}
	if strings.Join(gh.requests, " ") != tagPath+" "+relPath {
		t.Fatalf("requests %v, want the tag's ref and the release by tag", gh.requests)
	}
}

// A draft is neither a tag nor a published release, so both answer 404.
func TestCheckPassesOverADraft(t *testing.T) {
	code, _, stderr := newSetup(t, (&fakeGitHub{}).serve(t)).run(t)
	if code != 0 {
		t.Fatalf("code %d, stderr %q; want 0", code, stderr)
	}
}

func TestCheckRefusesABranchOtherThanMain(t *testing.T) {
	gh := &fakeGitHub{}
	s := newSetup(t, gh.serve(t))
	s.ref = "refs/heads/feature"
	code, stdout, stderr := s.run(t)
	wantRefusal(t, code, stdout, stderr, exitRefused, "only main", "refs/heads/feature")
	if len(gh.requests) != 0 {
		t.Fatalf("requests %v, want none", gh.requests)
	}
}

func TestCheckRefusesAReleasedVersion(t *testing.T) {
	gh := &fakeGitHub{status: map[string]int{tagPath: http.StatusOK, relPath: http.StatusOK}}
	code, stdout, stderr := newSetup(t, gh.serve(t)).run(t)
	wantRefusal(t, code, stdout, stderr, exitRefused, "0.2.1 already has a release")
}

func TestCheckRefusesAPublishedReleaseWithoutItsTag(t *testing.T) {
	gh := &fakeGitHub{status: map[string]int{relPath: http.StatusOK}}
	code, stdout, stderr := newSetup(t, gh.serve(t)).run(t)
	wantRefusal(t, code, stdout, stderr, exitRefused, "0.2.1 already has a release")
}

func TestCheckRefusesATagWithoutARelease(t *testing.T) {
	gh := &fakeGitHub{status: map[string]int{tagPath: http.StatusOK}}
	code, stdout, stderr := newSetup(t, gh.serve(t)).run(t)
	wantRefusal(t, code, stdout, stderr, exitRefused, "v0.2.1 exists without a published release")
}

func TestCheckRefusesAVersionWithoutASection(t *testing.T) {
	gh := &fakeGitHub{}
	s := newSetup(t, gh.serve(t))
	s.changelog = "# Changelog\n\n## 0.2.0\n\nOlder.\n"
	code, stdout, stderr := s.run(t)
	wantRefusal(t, code, stdout, stderr, exitRefused, "0.2.1 has no changelog section")
	if len(gh.requests) != 0 {
		t.Fatalf("requests %v, want none", gh.requests)
	}
}

func TestCheckFailsOnAnUnexpectedReply(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusForbidden} {
		gh := &fakeGitHub{status: map[string]int{tagPath: status}}
		api := gh.serve(t)
		code, stdout, stderr := newSetup(t, api).run(t)
		wantRefusal(t, code, stdout, stderr, exitError, api+tagPath, http.StatusText(status))
	}
}

func TestCheckFailsWithoutItsInputs(t *testing.T) {
	cases := map[string]func(*setup){
		"no repository": func(s *setup) { delete(s.env, "GITHUB_REPOSITORY") },
		"no token":      func(s *setup) { delete(s.env, "GITHUB_TOKEN") },
		"no API":        func(s *setup) { delete(s.env, "GITHUB_API_URL") },
		"empty VERSION": func(s *setup) { s.version = "\n" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := newSetup(t, (&fakeGitHub{}).serve(t))
			change(s)
			code, stdout, stderr := s.run(t)
			wantRefusal(t, code, stdout, stderr, exitError)
		})
	}
}

func TestCheckFailsOnAMissingFile(t *testing.T) {
	for _, flag := range []string{"-version", "-changelog"} {
		s := newSetup(t, (&fakeGitHub{}).serve(t))
		code, stdout, stderr := s.run(t, flag, s.path("missing"))
		wantRefusal(t, code, stdout, stderr, exitError, "missing")
	}
}

func TestCheckFailsWhenTheNotesCannotBeWritten(t *testing.T) {
	s := newSetup(t, (&fakeGitHub{}).serve(t))
	code, stdout, stderr := s.run(t, "-notes", s.path("no/such/dir/notes.md"))
	wantRefusal(t, code, stdout, stderr, exitError, "notes")
}

func TestRunFailsOnBadUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"no subcommand": nil, "unknown subcommand": {"publish"}, "unknown flag": {"check", "-nope"},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(args, func(string) string { return "" }, &stdout, &stderr)
			wantRefusal(t, code, stdout.String(), stderr.String(), exitError)
		})
	}
}

func TestCheckFailsWhenTheReleaseCannotBeRead(t *testing.T) {
	gh := &fakeGitHub{status: map[string]int{relPath: http.StatusBadGateway}}
	api := gh.serve(t)
	code, stdout, stderr := newSetup(t, api).run(t)
	wantRefusal(t, code, stdout, stderr, exitError, api+relPath)
}

func TestCheckFailsWhenGitHubCannotBeReached(t *testing.T) {
	for name, api := range map[string]string{
		"closed server": closedServer(t), "bad URL": "http://bad host",
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := newSetup(t, api).run(t)
			wantRefusal(t, code, stdout, stderr, exitError, "GitHub API")
		})
	}
}

// closedServer returns the URL of a test server that is already closed.
func closedServer(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()

	return srv.URL
}
