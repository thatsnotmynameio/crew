package bots

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// The bot ops of the acting tests: its app, its installation on crew and
// its bot's user id.
const (
	opsClientID = "Iv-ops"
	opsSlug     = "crew-ops"
	opsLogin    = "crew-ops[bot]"
	opsInst     = "11"
	opsUserID   = "456"
)

// actAPI is GitHub's REST API as Act calls it, served in memory, so it
// also runs in a synctest bubble. Its fields script the replies.
type actAPI struct {
	mu sync.Mutex
	// installStatus, when not 0, answers the installation lookup instead
	// of 200.
	installStatus int
	// tokenStatus, when not 0, answers every token call instead of 201.
	tokenStatus int
	// granted, when not nil, is the grant's permissions instead of those
	// asked.
	granted map[string]string
	// userBody, when not empty, answers the bot user lookup.
	userBody string

	tokenBodies []string
	userAuth    []string
	minted      int
}

func (g *actAPI) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/{owner}/{repo}/installation", g.lookup)
	mux.HandleFunc("POST /app/installations/{id}/access_tokens", g.token)
	mux.HandleFunc("GET /users/{login}", g.user)
	return mux
}

func (g *actAPI) lookup(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case g.installStatus != 0:
		reply(w, g.installStatus, `{"message":"`+http.StatusText(g.installStatus)+`"}`)
	case issuer(r) == opsClientID && r.PathValue("owner") == testOwner && r.PathValue("repo") == "crew":
		reply(w, http.StatusOK, `{"id":`+opsInst+`,"repository_selection":"selected"}`)
	default:
		reply(w, http.StatusNotFound, `{"message":"Not Found"}`)
	}
}

func (g *actAPI) token(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	g.tokenBodies = append(g.tokenBodies, string(body))
	switch {
	case g.tokenStatus != 0:
		reply(w, g.tokenStatus, `{"message":"Server Error"}`)
		return
	case r.PathValue("id") != opsInst:
		reply(w, http.StatusNotFound, `{"message":"Not Found"}`)
		return
	}
	g.minted++
	perms := g.granted
	if perms == nil {
		perms = permissions()
	}
	data, err := json.Marshal(struct {
		Token       string            `json:"token"`
		ExpiresAt   string            `json:"expires_at"`
		Permissions map[string]string `json:"permissions"`
	}{
		Token:     "ghs_" + opsInst + "n" + strconv.Itoa(g.minted),
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Permissions: perms,
	})
	if err != nil {
		reply(w, http.StatusInternalServerError, `{"message":"the fake's reply"}`)
		return
	}
	reply(w, http.StatusCreated, string(data))
}

func (g *actAPI) user(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.userAuth = append(g.userAuth, r.Header.Get("Authorization"))
	switch {
	case g.userBody != "":
		reply(w, http.StatusOK, g.userBody)
	case r.PathValue("login") == opsLogin:
		reply(w, http.StatusOK, `{"login":"`+opsLogin+`","id":`+opsUserID+`}`)
	default:
		reply(w, http.StatusNotFound, `{"message":"Not Found"}`)
	}
}

// issuer returns the iss of r's bearer JWT, or "" when it has none. It
// runs in whichever goroutine calls the API, so it cannot fail the test.
func issuer(r *http.Request) string {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Iss string `json:"iss"`
	}
	_ = json.Unmarshal(raw, &claims)
	return claims.Iss
}

// handlerTransport answers every request with a handler, in memory.
type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, r)
	return rec.Result(), nil
}

// actRun is one Act with every dependency faked: the repository crew of
// thatsnotmynameio, git 2.55 with the credential helpers in gitConf, none
// when empty, and your environment in env.
type actRun struct {
	api     *actAPI
	store   *Store
	opts    ActOptions
	env     map[string]string
	git     string
	gitConf string
	calls   []proc.Command
}

// newActRun returns a run that makes names act, the first one the default.
func newActRun(t *testing.T, names ...string) *actRun {
	t.Helper()
	r := &actRun{
		api: &actAPI{}, store: NewStore(t.TempDir()),
		env: map[string]string{"HOME": "/home/boss"}, git: "git version 2.55.0\n",
	}
	r.opts = ActOptions{
		Run: r.run, Store: r.store, Root: t.TempDir(), Names: names, TempDir: t.TempDir(),
		Client: NewClient("https://api.github.test", &http.Client{Transport: handlerTransport{r.api.handler()}}),
		Getenv: func(k string) string { return r.env[k] },
	}
	if len(names) > 0 {
		r.opts.Default = names[0]
	}
	return r
}

// run is your gh and git.
func (r *actRun) run(_ context.Context, c proc.Command) (proc.Output, error) {
	r.calls = append(r.calls, c)
	switch {
	case c.Name == "gh":
		return proc.Output{Stdout: []byte(`{"name":"crew","owner":{"login":"` + testOwner + `","id":42}}`)}, nil
	case slices.Equal(c.Args, []string{"version"}):
		return proc.Output{Stdout: []byte(r.git)}, nil
	case r.gitConf == "":
		return proc.Output{}, exitError{1}
	default:
		return proc.Output{Stdout: []byte(r.gitConf)}, nil
	}
}

// saveOps stores the bot ops for thatsnotmynameio.
func (r *actRun) saveOps(t *testing.T) {
	t.Helper()
	m := testBot(testOwner)
	m.Name, m.ClientID, m.Slug, m.AppName, m.BotLogin = "ops", opsClientID, opsSlug, opsSlug, opsLogin
	if err := r.store.Save(m); err != nil {
		t.Fatal(err)
	}
}

// writeOpsFile returns a setup that writes data as the file of the bot ops.
func writeOpsFile(data string) func(t *testing.T, r *actRun) {
	return func(t *testing.T, r *actRun) {
		t.Helper()
		path := r.store.Path(testOwner, "ops")
		if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), filePerm); err != nil {
			t.Fatal(err)
		}
	}
}

// saveOpsWithSlug returns a setup that stores the bot ops with the app
// slug slug.
func saveOpsWithSlug(slug string) func(t *testing.T, r *actRun) {
	return func(t *testing.T, r *actRun) {
		t.Helper()
		m := testBot(testOwner)
		m.Name, m.ClientID, m.Slug, m.AppName, m.BotLogin = "ops", opsClientID, slug, slug, opsLogin
		if err := r.store.Save(m); err != nil {
			t.Fatal(err)
		}
	}
}

// renewFailure is the warning of the bot ops, whose renewal failed with
// the error text cause.
func renewFailure(cause string) string {
	return "mate ops could not renew its token: " + cause + "; its sessions and checks fail once the " +
		"current token expires, and crew tries again every minute"
}

// checkFailing checks that a's renewal failures are want, none when it is
// nil, and that none holds a key or a token.
func checkFailing(t *testing.T, a *Acting, want map[string]string) {
	t.Helper()
	got := a.Failing()
	if !maps.Equal(got, want) {
		t.Errorf("Failing = %q, want %q", got, want)
	}
	checkNoSecret(t, strings.Join(slices.Collect(maps.Values(got)), "\n"))
}

// act runs Act and checks that neither its warnings nor its error hold a
// key or a token (R22). It closes the result when the test ends.
func (r *actRun) act(t *testing.T) (*Acting, error) {
	t.Helper()
	a, err := Act(context.Background(), r.opts)
	var all []string
	if err != nil {
		all = append(all, err.Error())
	}
	if a != nil {
		t.Cleanup(a.Close)
		all = append(all, a.Warnings...)
	}
	checkNoSecret(t, strings.Join(all, "\n"))
	return a, err
}

// mustAct runs Act and fails the test on an error.
func (r *actRun) mustAct(t *testing.T) *Acting {
	t.Helper()
	a, err := r.act(t)
	if err != nil {
		t.Fatalf("Act: %v", err)
	}
	return a
}

// checkNoSecret fails the test when text holds a token or the test key.
func checkNoSecret(t *testing.T, text string) {
	t.Helper()
	checkNoKey(t, text)
	if strings.Contains(text, "ghs_") {
		t.Fatal("the text holds a token") // never echo it
	}
}

// envValue returns the value of key in env, the last one winning.
func envValue(env []string, key string) string {
	value := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			value = v
		}
	}
	return value
}

// gitConfig returns the git config entries env sets, in order.
func gitConfig(env []string) []configEntry {
	count, _ := strconv.Atoi(envValue(env, "GIT_CONFIG_COUNT"))
	var entries []configEntry
	for i := range count {
		n := strconv.Itoa(i)
		if key := envValue(env, "GIT_CONFIG_KEY_"+n); key != "" {
			entries = append(entries, configEntry{key, envValue(env, "GIT_CONFIG_VALUE_"+n)})
		}
	}
	return entries
}

// readFile returns path's content, failing the test when it cannot.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// opsHosts is the hosts.yml of ops acting with token.
func opsHosts(token string) string {
	return "github.com:\n    users:\n        " + opsLogin + ":\n            oauth_token: " + token +
		"\n    git_protocol: https\n    oauth_token: " + token + "\n    user: " + opsLogin + "\n"
}

// checkGhDir checks that dir, under the run's parent directory tmp, is a
// gh config directory acting as ops with token, readable by its owner
// alone.
func checkGhDir(t *testing.T, tmp, dir, token string) {
	t.Helper()
	if rel, err := filepath.Rel(tmp, dir); err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("GH_CONFIG_DIR %q is not under the run's temporary directory", dir)
	}
	hosts, config := filepath.Join(dir, ghHostsFile), filepath.Join(dir, ghConfigFile)
	if got := readFile(t, hosts); got != opsHosts(token) {
		t.Errorf("hosts.yml =\n%s\nwant\n%s", got, opsHosts(token))
	}
	if got := readFile(t, config); got != "version: \"1\"\n" {
		t.Errorf("config.yml = %q, want version 1", got)
	}
	for _, p := range []string{hosts, config} {
		if m := mode(t, p); m != filePerm {
			t.Errorf("%s has mode %v, want 0600", p, m)
		}
	}
	for d := dir; d != filepath.Clean(tmp); d = filepath.Dir(d) {
		if m := mode(t, d); m != dirPerm {
			t.Errorf("%s has mode %v, want 0700", d, m)
		}
	}
}
