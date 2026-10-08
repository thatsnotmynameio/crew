package upgrade

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

func TestResolveAPIAcceptsOnlyALoopbackOverride(t *testing.T) {
	api, err := ResolveAPI("")
	if err != nil || api != (API{Base: DefaultAPI, Host: "github.com"}) {
		t.Errorf("ResolveAPI(\"\") = %+v, %v, want GitHub's API", api, err)
	}
	for value, want := range map[string]API{
		"http://127.0.0.1:1234": {Base: "http://127.0.0.1:1234", Host: "127.0.0.1", Override: true},
		"https://[::1]:8443":    {Base: "https://[::1]:8443", Host: "::1", Override: true},
		"http://127.0.0.1":      {Base: "http://127.0.0.1", Host: "127.0.0.1", Override: true},
	} {
		got, err := ResolveAPI(value)
		if err != nil || got != want {
			t.Errorf("ResolveAPI(%q) = %+v, %v, want %+v", value, got, err, want)
		}
	}
	for _, value := range []string{
		"http://localhost:1234", "https://example.com", "http://10.0.0.1", "http://127.0.0.1@evil.example",
		"http://127.0.0.1:1/path", "http://127.0.0.1:1/?q=1", "http://127.0.0.1:1#f", "ftp://127.0.0.1", "127.0.0.1:1",
		"http://127.0.0.1:1?", "http://[::1", "http:127.0.0.1",
	} {
		_, err := ResolveAPI(value)
		if _, ok := errors.AsType[*EnvError](err); !ok || !strings.Contains(err.Error(), APIEnv) {
			t.Errorf("ResolveAPI(%q) = %v, want an *EnvError naming %s", value, err, APIEnv)
		}
	}
}

func TestNoticeNamesTheOverride(t *testing.T) {
	if got := (API{Base: DefaultAPI, Host: "github.com"}).Notice(); got != "" {
		t.Errorf("Notice of GitHub's API = %q, want none", got)
	}
	for _, value := range []string{"http://127.0.0.1:1234", "https://[::1]:8443"} {
		api, err := ResolveAPI(value)
		if err != nil {
			t.Fatalf("ResolveAPI(%q) = %v", value, err)
		}
		got := api.Notice()
		if !strings.Contains(got, value) || !strings.Contains(got, APIEnv) || strings.Contains(got, "\n") {
			t.Errorf("Notice = %q, want one line naming %s and %s", got, value, APIEnv)
		}
	}
}

// scriptedGh is a proc.Runner that records the gh command it ran and
// answers out, stderr and err.
type scriptedGh struct {
	ran    []proc.Command
	out    string
	stderr string
	err    error
}

func (s *scriptedGh) run(_ context.Context, c proc.Command) (proc.Output, error) {
	s.ran = append(s.ran, c)
	return proc.Output{Stdout: []byte(s.out), Stderr: []byte(s.stderr)}, s.err
}

func TestGHTokenAsksGhForTheAPIsHost(t *testing.T) {
	gh := &scriptedGh{out: testToken + "\n"}
	token, reason := GHToken(gh.run)(t.Context(), API{Base: DefaultAPI, Host: "github.com"})
	if token != testToken || reason != "" {
		t.Errorf("token = %q, %q, want %q and no reason", token, reason, testToken)
	}
	c := gh.ran[0]
	want := []string{"auth", "token", "--hostname", "github.com"}
	if c.Name != "gh" || !slices.Equal(c.Args, want) || len(c.Unset) != 0 {
		t.Errorf("ran %+v, want gh auth token --hostname github.com", c)
	}
}

func TestGHTokenUnderTheOverrideAsksForItsHostWithoutTheTokenVariables(t *testing.T) {
	gh := &scriptedGh{}
	GHToken(gh.run)(t.Context(), API{Base: "http://127.0.0.1:1", Host: "127.0.0.1", Override: true})
	c := gh.ran[0]
	if !slices.Equal(c.Args, []string{"auth", "token", "--hostname", "127.0.0.1"}) {
		t.Errorf("args = %q, want the override's host", c.Args)
	}
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		if !slices.Contains(c.Unset, name) {
			t.Errorf("Unset = %q, want %s in it", c.Unset, name)
		}
	}
}

func TestGHTokenGivesTheReasonThereIsNone(t *testing.T) {
	tests := []struct {
		name string
		gh   *scriptedGh
		want string
	}{
		{name: "gh missing", gh: &scriptedGh{err: fmt.Errorf("start gh: %w", exec.ErrNotFound)},
			want: "gh is not on PATH"},
		{name: "logged out", gh: &scriptedGh{
			stderr: "no oauth token found for github.com\nrun gh auth login\n",
			err:    errors.New("gh: exit status 1: no oauth token found for github.com"),
		}, want: "no oauth token found for github.com"},
		{name: "silent failure", gh: &scriptedGh{err: errors.New("gh: exit status 4")},
			want: "gh: exit status 4"},
		{name: "timeout", gh: &scriptedGh{err: fmt.Errorf("gh: %w", context.DeadlineExceeded)},
			want: "gh auth token timed out after 10s"},
		{name: "empty output", gh: &scriptedGh{out: "  \n"}, want: "gh auth token printed no token"},
		{name: "control characters", gh: &scriptedGh{stderr: "bad\x1b[2Jlogin", err: errors.New("gh: exit status 1")},
			want: "bad[2Jlogin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, reason := GHToken(tt.gh.run)(t.Context(), API{Host: "github.com"})
			if token != "" || reason != tt.want {
				t.Errorf("token = %q, %q, want none and %q", token, reason, tt.want)
			}
		})
	}
}

func TestTheTokenStaysOutOfURLsArgumentsAndErrors(t *testing.T) {
	const probe = "gho_probe_never_shown"
	f := &fakeGitHub{t: t, status: http.StatusUnauthorized, message: "Bad credentials"}
	_, err := f.serve(probe).Release(t.Context(), "v0.5.0")
	if err == nil || strings.Contains(err.Error(), probe) {
		t.Errorf("Release = %v, want an error without the token", err)
	}
	for _, r := range f.seen() {
		if strings.Contains(r.URL.String(), probe) {
			t.Errorf("URL %q holds the token", r.URL)
		}
	}
	gh := &scriptedGh{out: probe}
	GHToken(gh.run)(t.Context(), API{Host: "github.com"})
	if strings.Contains(strings.Join(gh.ran[0].Args, " "), probe) {
		t.Errorf("gh args %q hold the token", gh.ran[0].Args)
	}
	limited := &fakeGitHub{t: t, status: http.StatusTooManyRequests, header: http.Header{"Retry-After": {"60"}}}
	if _, err := limited.serve(probe).Release(t.Context(), ""); err == nil || strings.Contains(err.Error(), probe) {
		t.Errorf("Release = %v, want an error without the token", err)
	}
}
