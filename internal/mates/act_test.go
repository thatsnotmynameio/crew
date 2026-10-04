package mates

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

func TestActGivesAStoredInstalledMateItsGhDirectory(t *testing.T) {
	r := newActRun(t, "ops", "developer")
	r.saveOps(t)
	a := r.mustAct(t)
	if len(a.Mates) != 1 {
		t.Fatalf("Mates = %+v, want ops alone", a.Mates)
	}
	ops := a.Mates[0]
	if ops.Name != "ops" || ops.Login != "crew-ops[bot]" ||
		!slices.Equal(ops.Unset,
			[]string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_HOST"}) {
		t.Errorf("ops = %+v, want crew-ops[bot] with the token variables unset", ops)
	}
	for _, env := range [][]string{ops.Env, ops.WriterEnv} {
		checkGhDir(t, r.opts.TempDir, envValue(env, "GH_CONFIG_DIR"), "ghs_11n1")
	}
	if envValue(ops.Env, "GH_CONFIG_DIR") == envValue(ops.WriterEnv, "GH_CONFIG_DIR") || len(ops.WriterEnv) != 1 {
		t.Errorf("WriterEnv = %q, want only a directory of crew's own", ops.WriterEnv)
	}
	hook := []configEntry{{"hook.crew-co-author.event", "commit-msg"}, {"hook.crew-co-author.command",
		"git interpret-trailers --in-place --if-exists addIfDifferent --trailer " +
			"'Co-authored-by: crew-ops[bot] <456+crew-ops[bot]@users.noreply.github.com>'"}}
	if !slices.Equal(gitConfig(ops.Env), hook) {
		t.Errorf("git config = %q, want the co-author hook alone", gitConfig(ops.Env))
	}
	want := []string{"mate developer has no key on this machine for thatsnotmynameio; " +
		"run `crew mates create developer` in this repository"}
	if !slices.Equal(a.Warnings, want) {
		t.Errorf("Warnings = %q, want %q", a.Warnings, want)
	}
	if !slices.Equal(a.Logins, []string{opsLogin}) {
		t.Errorf("Logins = %q, want ops's alone, as developer has no key here", a.Logins)
	}
}

func TestActAsksForATokenOfTheRepositoryWithCrewsPermissions(t *testing.T) {
	r := newActRun(t, "ops")
	r.saveOps(t)
	r.mustAct(t)
	var body struct {
		Repositories []string          `json:"repositories"`
		Permissions  map[string]string `json:"permissions"`
	}
	if len(r.api.tokenBodies) != 1 || json.Unmarshal([]byte(r.api.tokenBodies[0]), &body) != nil ||
		!slices.Equal(body.Repositories, []string{"crew"}) || !maps.Equal(body.Permissions, permissions()) {
		t.Errorf("token requests = %q, want one for crew with the manifest's permissions", r.api.tokenBodies)
	}
	if !slices.Equal(r.api.userAuth, []string{"token ghs_11n1"}) {
		t.Error("the bot user lookup did not send the installation token alone")
	}
}

// statelessToken has the shape of GitHub's stateless installation tokens,
// ghs_APPID_JWT, whose JWT holds dots and base64url hyphens.
const statelessToken = "ghs_12345_header-_.payload-_.signature-_"

func TestActWritesAStatelessToken(t *testing.T) {
	r := newActRun(t, "ops")
	r.saveOps(t)
	r.opts.mint = func(context.Context, Mate, int64, string) (Grant, error) {
		return Grant{Token: statelessToken, ExpiresAt: time.Now().Add(time.Hour), Permissions: permissions()}, nil
	}
	a := r.mustAct(t)
	if len(a.Mates) != 1 || len(a.Warnings) != 0 {
		t.Fatalf("Act = %+v, %q; want ops acting with no warning", a.Mates, a.Warnings)
	}
	checkGhDir(t, r.opts.TempDir, envValue(a.Mates[0].Env, "GH_CONFIG_DIR"), statelessToken)
}

func TestUsableToken(t *testing.T) {
	tests := []struct {
		token Token
		want  bool
	}{
		{Token("ghs_" + strings.Repeat("A", 36)), true},
		{statelessToken, true},
		{"", false},
		{"ghs_", false},
		{"-", false},
		{"abc.def", false},
		{"ghs_a\nuser: someone", false},
		{"ghs_a b", false},
		{"ghs_a:b", false},
		{"ghs_a#b", false},
		{`ghs_a"b`, false},
		{"ghs_a'b", false},
	}
	for _, tt := range tests {
		if got := usableToken(tt.token); got != tt.want {
			t.Errorf("usableToken(%q) = %v, want %v", string(tt.token), got, tt.want)
		}
	}
}

func TestActWarnsAndActsAsTheBossForAnUnusableMate(t *testing.T) {
	const asked = "actions:read checks:read contents:read issues:write metadata:read pull_requests:write statuses:read"
	tests := []struct {
		name          string
		saved         bool
		installStatus int
		tokenStatus   int
		granted       map[string]string
		want          string
	}{
		{name: "AE10 no file",
			want: "mate ops has no key on this machine for thatsnotmynameio; run `crew mates create ops` in this repository"},
		{name: "AE3 not installed", saved: true, installStatus: http.StatusNotFound,
			want: "mate ops is not installed on thatsnotmynameio/crew; run `crew mates create ops` in this repository"},
		{name: "key rejected", saved: true, installStatus: http.StatusUnauthorized,
			want: "GitHub rejected the key of mate ops; delete <path> and run `crew mates create ops` in this repository"},
		{name: "token call fails", saved: true, tokenStatus: http.StatusInternalServerError,
			want: "mate ops could not get a token: mint a token of ops for crew: GitHub answered 500 Internal Server Error: " +
				"Server Error; crew acts as the boss in its place this run"},
		{name: "more granted", saved: true, granted: map[string]string{
			"actions": permRead, "administration": permWrite, "checks": permRead, "contents": permRead,
			"issues": permWrite, "metadata": permRead, "pull_requests": permWrite, "statuses": permRead,
		}, want: "mate ops could not get a token: GitHub granted actions:read administration:write checks:read " +
			"contents:read issues:write metadata:read pull_requests:write statuses:read, not the " + asked +
			" crew asked for; crew acts as the boss in its place this run"},
		{name: "fewer granted", saved: true, granted: map[string]string{"metadata": permRead},
			want: "mate ops could not get a token: GitHub granted metadata:read, not the " + asked +
				" crew asked for; crew acts as the boss in its place this run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newActRun(t, "ops")
			if tt.saved {
				r.saveOps(t)
			}
			r.api.installStatus, r.api.tokenStatus, r.api.granted = tt.installStatus, tt.tokenStatus, tt.granted
			a := r.mustAct(t)
			want := strings.ReplaceAll(tt.want, "<path>", r.store.Path(testOwner, "ops"))
			if len(a.Mates) != 0 || !slices.Equal(a.Warnings, []string{want}) {
				t.Errorf("Act = %+v, %q; want no mate and %q", a.Mates, a.Warnings, want)
			}
			// A stored mate that cannot act still has its issues taken.
			if wantLogins := []string{opsLogin}; tt.saved != slices.Equal(a.Logins, wantLogins) {
				t.Errorf("Logins = %q, want ops's login exactly when its file is stored", a.Logins)
			}
		})
	}
}

func TestActRefusesAnInvalidNameAndLeavesNothing(t *testing.T) {
	r := newActRun(t, "ops", "Ops")
	r.saveOps(t)
	if _, err := r.act(t); !errors.As(err, new(*EnvError)) {
		t.Errorf("Act with Ops = %v, want an EnvError", err)
	}
	if entries, _ := os.ReadDir(r.opts.TempDir); len(entries) != 0 || len(r.calls) != 0 {
		t.Errorf("Act left %v and ran %v, want nothing", entries, r.calls)
	}
}

func TestActRefusesARunDirectoryInsideTheRepository(t *testing.T) {
	r := newActRun(t, "ops")
	r.saveOps(t)
	r.opts.TempDir = filepath.Join(r.opts.Root, "tmp")
	if err := os.Mkdir(r.opts.TempDir, dirPerm); err != nil {
		t.Fatal(err)
	}
	var steps []string
	r.opts.Step = func(step string) { steps = append(steps, step) }
	if _, err := r.act(t); !errors.As(err, new(*EnvError)) || !strings.Contains(err.Error(), "inside the repository") {
		t.Errorf("Act = %v, want an EnvError saying the directory is inside the repository", err)
	}
	// The boot log ends with the step that failed.
	if len(steps) == 0 || steps[len(steps)-1] != "making a private directory for the mates' tokens" {
		t.Errorf("steps = %q, want them to end with making the tokens' directory", steps)
	}
	if entries, _ := os.ReadDir(r.opts.TempDir); len(entries) != 0 {
		t.Errorf("Act left %v", entries)
	}
}

func TestActKeepsTokensUnderTheRuntimeDirectory(t *testing.T) {
	r := newActRun(t, "ops")
	r.saveOps(t)
	r.env["XDG_RUNTIME_DIR"], r.opts.TempDir = r.opts.TempDir, ""
	a := r.mustAct(t)
	dir := envValue(a.Mates[0].Env, "GH_CONFIG_DIR")
	if !strings.HasPrefix(dir, r.env["XDG_RUNTIME_DIR"]+string(filepath.Separator)) {
		t.Errorf("GH_CONFIG_DIR = %s, want it under XDG_RUNTIME_DIR", dir)
	}
}

func TestActFailsWhenGhCannotResolveTheRepository(t *testing.T) {
	r := newActRun(t, "ops")
	r.opts.Run = func(context.Context, proc.Command) (proc.Output, error) {
		return proc.Output{}, errors.New("gh: not logged in")
	}
	var steps []string
	r.opts.Step = func(step string) { steps = append(steps, step) }
	if _, err := r.act(t); !errors.As(err, new(*EnvError)) {
		t.Errorf("Act = %v, want an EnvError", err)
	}
	if entries, _ := os.ReadDir(r.opts.TempDir); len(entries) != 0 {
		t.Errorf("Act left %v", entries)
	}
	if want := []string{"resolving the repository for the mates"}; !slices.Equal(steps, want) {
		t.Errorf("steps = %q, want %q, the step that failed last", steps, want)
	}
}

func TestActWithoutNamesDoesNothing(t *testing.T) {
	r := newActRun(t)
	var steps []string
	r.opts.Step = func(step string) { steps = append(steps, step) }
	a := r.mustAct(t)
	a.Close()
	if len(a.Mates) != 0 || len(a.Warnings) != 0 || len(r.calls) != 0 || len(steps) != 0 {
		t.Errorf("Act = %+v after %v and the steps %q, want nothing", a, r.calls, steps)
	}
}

func TestActReportsEachStepAsItStarts(t *testing.T) {
	r := newActRun(t, "alice", "bob")
	var steps []string
	r.opts.Step = func(step string) { steps = append(steps, step) }
	a := r.mustAct(t)
	// Neither mate is stored here, so both end up warnings, yet each had its
	// line: its step started.
	if len(a.Warnings) != 2 {
		t.Errorf("Warnings = %q, want one for each mate", a.Warnings)
	}
	want := []string{"resolving the repository for the mates", "checking git for the mates",
		"making a private directory for the mates' tokens", "making mate alice act", "making mate bob act"}
	if !slices.Equal(steps, want) {
		t.Errorf("steps = %q, want %q", steps, want)
	}
}

func TestActWithoutACoAuthorStillActs(t *testing.T) {
	tests := []struct {
		name   string
		script func(r *actRun)
		want   string
	}{
		{name: "lookup fails", script: func(r *actRun) { r.api.userBody = `{"message":"oops"}` },
			want: "mate ops's commits carry no co-author: crew could not find the user id of crew-ops[bot] " +
				"(GitHub gave crew-ops[bot] the user id 0); restart crew to try again"},
		{name: "non-numeric id", script: func(r *actRun) { r.api.userBody = `{"id":"456"}` },
			want: "mate ops's commits carry no co-author: crew could not find the user id of crew-ops[bot] " +
				"(find the user of crew-ops[bot]: GitHub API: unreadable reply: json: cannot unmarshal string " +
				"into Go struct field .id of type int64); restart crew to try again"},
		{name: "old git", script: func(r *actRun) { r.git = "git version 2.53.0\n" },
			want: "mate ops's commits carry no co-author: git version 2.53.0 is older than git 2.54, " +
				"which runs the co-author hook; update git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newActRun(t, "ops")
			r.saveOps(t)
			tt.script(r)
			a := r.mustAct(t)
			if len(a.Mates) != 1 || gitConfig(a.Mates[0].Env) != nil {
				t.Errorf("Mates = %+v, want ops without a hook", a.Mates)
			}
			if !slices.Equal(a.Warnings, []string{tt.want}) {
				t.Errorf("Warnings = %q, want %q", a.Warnings, tt.want)
			}
		})
	}
}

func TestActPinsTheBossCredentialHelper(t *testing.T) {
	tests := []struct {
		name, config string
		want         []configEntry
	}{
		{name: "github.com helper", config: "credential.https://github.com.helper !/opt/gh auth git-credential\n",
			want: []configEntry{{githubHelperKey, ""},
				{githubHelperKey, `!GH_CONFIG_DIR='/b o'\''ss/gh' /opt/gh auth git-credential`}}},
		{name: "helper", config: "credential.helper /usr/bin/gh auth git-credential\n",
			want: []configEntry{{githubHelperKey, ""},
				{githubHelperKey, `!GH_CONFIG_DIR='/b o'\''ss/gh' /usr/bin/gh auth git-credential`}}},
		{name: "osxkeychain", config: "credential.helper osxkeychain\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newActRun(t, "ops")
			r.saveOps(t)
			r.gitConf, r.env["GH_CONFIG_DIR"] = tt.config, "/b o'ss/gh"
			a := r.mustAct(t)
			got := gitConfig(a.Mates[0].Env)[2:]
			if !slices.Equal(got, tt.want) {
				t.Errorf("credential entries = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestActStartsAfterTheInheritedGitConfig(t *testing.T) {
	r := newActRun(t, "ops")
	r.saveOps(t)
	r.env["GIT_CONFIG_COUNT"] = "2"
	env := r.mustAct(t).Mates[0].Env
	if envValue(env, "GIT_CONFIG_COUNT") != "4" || envValue(env, "GIT_CONFIG_KEY_2") != "hook.crew-co-author.event" ||
		envValue(env, "GIT_CONFIG_KEY_0") != "" {
		t.Errorf("env = %q, want crew's entries at 2 and 3", env)
	}
}

func TestRenewRewritesBothDirectoriesOfTheDefaultMate(t *testing.T) {
	r := newActRun(t, "ops")
	r.saveOps(t)
	a := r.mustAct(t)
	dirs := []string{envValue(a.Mates[0].Env, "GH_CONFIG_DIR"), envValue(a.Mates[0].WriterEnv, "GH_CONFIG_DIR")}
	if err := a.Renew(context.Background(), "ops"); err != nil {
		t.Fatalf("Renew: %v", err)
	}
	for _, dir := range dirs {
		if got := readFile(t, filepath.Join(dir, "hosts.yml")); got != opsHosts("ghs_11n2") {
			t.Errorf("%s/hosts.yml =\n%s\nwant the new token", dir, got)
		}
	}
	r.api.tokenStatus = http.StatusBadGateway
	err := a.Renew(context.Background(), "ops")
	if err == nil {
		t.Fatal("Renew on a failing GitHub = nil error, want one")
	}
	checkNoSecret(t, err.Error())
	for _, dir := range dirs {
		if got := readFile(t, filepath.Join(dir, "hosts.yml")); got != opsHosts("ghs_11n2") {
			t.Errorf("%s/hosts.yml =\n%s\nwant the last token kept", dir, got)
		}
	}
	if err := a.Renew(context.Background(), "developer"); err == nil {
		t.Error("Renew of a mate that does not act = nil error, want one")
	}
}

func TestRenewalLoopRenewsBeforeTheTokenExpires(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newActRun(t, "ops")
		r.saveOps(t)
		var mu sync.Mutex
		mints := 0
		r.opts.mint = func(context.Context, Mate, int64, string) (Grant, error) {
			mu.Lock()
			defer mu.Unlock()
			mints++
			if mints == 2 {
				return Grant{}, errors.New("GitHub answered 502 Bad Gateway")
			}
			return Grant{Token: Token("ghs_" + strconv.Itoa(mints)), ExpiresAt: time.Now().Add(time.Hour),
				Permissions: permissions()}, nil
		}
		a := r.mustAct(t)
		hosts := filepath.Join(envValue(a.Mates[0].Env, "GH_CONFIG_DIR"), "hosts.yml")
		check := func(token string) {
			t.Helper()
			synctest.Wait()
			if got := readFile(t, hosts); got != opsHosts(token) {
				t.Errorf("after %v hosts.yml =\n%s\nwant %s", time.Since(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)), got, token)
			}
		}
		// AE5: at 50 minutes 10 remain; the first check with fewer, at 51,
		// renews, and its failure is tried again at 52.
		time.Sleep(50*time.Minute + 30*time.Second)
		check("ghs_1")
		time.Sleep(time.Minute)
		check("ghs_1")
		time.Sleep(time.Minute)
		check("ghs_3")
		// Three hours on, every token was renewed in time.
		time.Sleep(3 * time.Hour)
		check("ghs_6")
		a.Close()
		if _, err := os.Stat(filepath.Dir(filepath.Dir(filepath.Dir(hosts)))); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Close left the run directory: %v", err)
		}
		a.Close()
		mu.Lock()
		defer mu.Unlock()
		if mints != 6 {
			t.Errorf("minted %d tokens, want 6", mints)
		}
	})
}
