package bots

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

func TestActGivesAStoredInstalledBotItsGhDirectory(t *testing.T) {
	r := newActRun(t, "ops", "developer")
	r.saveOps(t)
	a := r.mustAct(t)
	if len(a.Bots) != 1 {
		t.Fatalf("Bots = %+v, want ops alone", a.Bots)
	}
	ops := a.Bots[0]
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
	if want := map[string]string{"developer": "no key"}; !maps.Equal(a.Unable, want) {
		t.Errorf("Unable = %q, want %q", a.Unable, want)
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
	r.opts.mint = func(context.Context, Bot, int64, string) (Grant, error) {
		return Grant{Token: statelessToken, ExpiresAt: time.Now().Add(time.Hour), Permissions: permissions()}, nil
	}
	a := r.mustAct(t)
	if len(a.Bots) != 1 || len(a.Warnings) != 0 {
		t.Fatalf("Act = %+v, %q; want ops acting with no warning", a.Bots, a.Warnings)
	}
	checkGhDir(t, r.opts.TempDir, envValue(a.Bots[0].Env, "GH_CONFIG_DIR"), statelessToken)
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

// asked is the permissions crew asks for, as its warnings write them.
const asked = "actions:read checks:read contents:read issues:write metadata:read pull_requests:write statuses:read"

// unusableBots are the ways the bot ops cannot act: how its file and
// GitHub's replies are scripted, then its warning, <path> standing for its
// file's, and its short reason.
var unusableBots = []struct {
	name          string
	saved         bool
	file          func(t *testing.T, r *actRun)
	installStatus int
	tokenStatus   int
	granted       map[string]string
	want, reason  string
}{
	{name: "AE10 no file", reason: "no key",
		want: "mate ops has no key on this machine for thatsnotmynameio; run `crew mates create ops` in this repository"},
	{name: "unreadable file", file: writeOpsFile("{"), reason: "bad key file",
		want: "mate ops cannot act: the mate file <path> is not valid JSON; " +
			"delete its file and run `crew mates create ops` in this repository"},
	{name: "invalid slug", file: saveOpsWithSlug("Crew Ops"), reason: "bad key file",
		want: "mate ops cannot act: its file <path> holds an invalid app slug; " +
			"delete it and run `crew mates create ops` in this repository"},
	{name: "AE3 not installed", saved: true, installStatus: http.StatusNotFound, reason: "not installed",
		want: "mate ops is not installed on thatsnotmynameio/crew; run `crew mates create ops` in this repository"},
	{name: "key rejected", saved: true, installStatus: http.StatusUnauthorized, reason: "key rejected",
		want: "GitHub rejected the key of mate ops; delete <path> and run `crew mates create ops` in this repository"},
	{name: "installation lookup fails", saved: true, installStatus: http.StatusInternalServerError,
		reason: "no token", want: "mate ops could not get a token: find the installation of ops on " +
			"thatsnotmynameio/crew: GitHub answered 500 Internal Server Error: Internal Server Error; " +
			"crew acts as the boss in its place this run"},
	{name: "key rejected by the token call", saved: true, tokenStatus: http.StatusUnauthorized,
		reason: "key rejected",
		want:   "GitHub rejected the key of mate ops; delete <path> and run `crew mates create ops` in this repository"},
	{name: "token call fails", saved: true, tokenStatus: http.StatusInternalServerError, reason: "no token",
		want: "mate ops could not get a token: mint a token of ops for crew: GitHub answered 500 Internal Server Error: " +
			"Server Error; crew acts as the boss in its place this run"},
	{name: "more granted", saved: true, granted: map[string]string{
		"actions": permRead, "administration": permWrite, "checks": permRead, "contents": permRead,
		"issues": permWrite, "metadata": permRead, "pull_requests": permWrite, "statuses": permRead,
	}, reason: "no token", want: "mate ops could not get a token: GitHub granted actions:read administration:write " +
		"checks:read contents:read issues:write metadata:read pull_requests:write statuses:read, not the " + asked +
		" crew asked for; crew acts as the boss in its place this run"},
	{name: "fewer granted", saved: true, granted: map[string]string{"metadata": permRead}, reason: "no token",
		want: "mate ops could not get a token: GitHub granted metadata:read, not the " + asked +
			" crew asked for; crew acts as the boss in its place this run"},
}

func TestActWarnsAndActsAsYouForAnUnusableBot(t *testing.T) {
	for _, tt := range unusableBots {
		t.Run(tt.name, func(t *testing.T) {
			r := newActRun(t, "ops")
			if tt.saved {
				r.saveOps(t)
			}
			if tt.file != nil {
				tt.file(t, r)
			}
			r.api.installStatus, r.api.tokenStatus, r.api.granted = tt.installStatus, tt.tokenStatus, tt.granted
			checkUnusable(t, r.mustAct(t), tt.want, tt.reason, r.store.Path(testOwner, "ops"), tt.saved)
		})
	}
}

// checkUnusable checks that a holds no acting bot, only the warning want,
// with <path> standing for path, and ops's short reason, and ops's login
// exactly when its file is stored.
func checkUnusable(t *testing.T, a *Acting, want, reason, path string, saved bool) {
	t.Helper()
	want = strings.ReplaceAll(want, "<path>", path)
	if len(a.Bots) != 0 || !slices.Equal(a.Warnings, []string{want}) {
		t.Errorf("Act = %+v, %q; want no mate and %q", a.Bots, a.Warnings, want)
	}
	if wantUnable := map[string]string{"ops": reason}; !maps.Equal(a.Unable, wantUnable) {
		t.Errorf("Unable = %q, want %q", a.Unable, wantUnable)
	}
	// A stored bot that cannot act still has its issues taken.
	if wantLogins := []string{opsLogin}; saved != slices.Equal(a.Logins, wantLogins) {
		t.Errorf("Logins = %q, want ops's login exactly when its file is stored", a.Logins)
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
	dir := envValue(a.Bots[0].Env, "GH_CONFIG_DIR")
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
	if len(a.Bots) != 0 || len(a.Warnings) != 0 || len(a.Unable) != 0 || len(r.calls) != 0 || len(steps) != 0 {
		t.Errorf("Act = %+v after %v and the steps %q, want nothing", a, r.calls, steps)
	}
	if failing := a.Failing(); failing == nil || len(failing) != 0 {
		t.Errorf("Failing = %#v, want an empty map", failing)
	}
}

func TestActReportsEachStepAsItStarts(t *testing.T) {
	r := newActRun(t, "alice", "bob")
	var steps []string
	r.opts.Step = func(step string) { steps = append(steps, step) }
	a := r.mustAct(t)
	// Neither bot is stored here, so both end up warnings, yet each had its
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
			if len(a.Bots) != 1 || gitConfig(a.Bots[0].Env) != nil {
				t.Errorf("Bots = %+v, want ops without a hook", a.Bots)
			}
			if !slices.Equal(a.Warnings, []string{tt.want}) {
				t.Errorf("Warnings = %q, want %q", a.Warnings, tt.want)
			}
			// A bot without a co-author still acts, so it has no reason.
			if len(a.Unable) != 0 {
				t.Errorf("Unable = %q, want none", a.Unable)
			}
		})
	}
}

func TestActPinsYourCredentialHelper(t *testing.T) {
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
			got := gitConfig(a.Bots[0].Env)[2:]
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
	env := r.mustAct(t).Bots[0].Env
	if envValue(env, "GIT_CONFIG_COUNT") != "4" || envValue(env, "GIT_CONFIG_KEY_2") != "hook.crew-co-author.event" ||
		envValue(env, "GIT_CONFIG_KEY_0") != "" {
		t.Errorf("env = %q, want crew's entries at 2 and 3", env)
	}
}

func TestRenewRewritesBothDirectoriesOfTheDefaultBot(t *testing.T) {
	r := newActRun(t, "ops")
	r.saveOps(t)
	a := r.mustAct(t)
	dirs := []string{envValue(a.Bots[0].Env, "GH_CONFIG_DIR"), envValue(a.Bots[0].WriterEnv, "GH_CONFIG_DIR")}
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
		r.opts.mint = func(context.Context, Bot, int64, string) (Grant, error) {
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
		hosts := filepath.Join(envValue(a.Bots[0].Env, "GH_CONFIG_DIR"), "hosts.yml")
		// check checks the token in hosts.yml, and the renewal failures.
		check := func(token string, failing map[string]string) {
			t.Helper()
			synctest.Wait()
			if got := readFile(t, hosts); got != opsHosts(token) {
				t.Errorf("after %v hosts.yml =\n%s\nwant %s", time.Since(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)), got, token)
			}
			checkFailing(t, a, failing)
		}
		// AE5: at 50 minutes 10 remain; the first check with fewer, at 51,
		// renews, and its failure, which names the bot until a renewal
		// succeeds, is tried again at 52.
		time.Sleep(50*time.Minute + 30*time.Second)
		check("ghs_1", nil)
		time.Sleep(time.Minute)
		check("ghs_1", map[string]string{"ops": renewFailure("GitHub answered 502 Bad Gateway")})
		time.Sleep(time.Minute)
		check("ghs_3", nil)
		// Three hours on, every token was renewed in time.
		time.Sleep(3 * time.Hour)
		check("ghs_6", nil)
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

func TestRenewReportsItsFailureUntilARenewalSucceeds(t *testing.T) {
	tests := []struct {
		name string
		fail error
		want string
	}{
		{name: "GitHub fails", fail: errors.New("GitHub answered 502 Bad Gateway"),
			want: renewFailure("GitHub answered 502 Bad Gateway")},
		{name: "key rejected", fail: fmt.Errorf("%w: GitHub answered 401", ErrKeyRejected),
			want: "GitHub rejected the key of mate ops; delete <path> and run `crew mates create ops` in this repository"},
		{name: "one line, no control character", fail: errors.New("bad\x1b[31m gate\tway\r\nsecond line"),
			want: renewFailure("bad[31m gateway")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newActRun(t, "ops")
			r.saveOps(t)
			var fail error
			r.opts.mint = func(context.Context, Bot, int64, string) (Grant, error) {
				if fail != nil {
					return Grant{}, fail
				}
				return Grant{Token: "ghs_1", ExpiresAt: time.Now().Add(time.Hour), Permissions: permissions()}, nil
			}
			a := r.mustAct(t)
			fail = tt.fail
			if err := a.Renew(context.Background(), "ops"); err == nil {
				t.Fatal("Renew on a failing mint = nil error, want one")
			}
			want := map[string]string{"ops": strings.ReplaceAll(tt.want, "<path>", r.store.Path(testOwner, "ops"))}
			checkFailing(t, a, want)
			// The map is a copy: changing it changes no later reading.
			failing := a.Failing()
			failing["ops"], failing["developer"] = "changed", "added"
			checkFailing(t, a, want)
			fail = nil
			if err := a.Renew(context.Background(), "ops"); err != nil {
				t.Fatalf("Renew: %v", err)
			}
			checkFailing(t, a, nil)
		})
	}
}

func TestFailingOfNoBotsIsEmpty(t *testing.T) {
	if failing := new(Acting).Failing(); failing == nil || len(failing) != 0 {
		t.Errorf("Failing = %#v, want an empty map", failing)
	}
}
