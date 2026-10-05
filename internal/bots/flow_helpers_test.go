package bots

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// The fixtures of the create flow's tests.
const (
	testOwner = "thatsnotmynameio"
	testWeb   = "https://github.test"
	testCode  = "c0de"
	// testInstallURL is where the test bot crew-tester is installed.
	testInstallURL = testWeb + "/apps/crew-tester/installations/new"
)

// fakeGitHub is GitHub's REST API as the create flow calls it. Its fields
// script the replies; its counters say what was called.
type fakeGitHub struct {
	mu sync.Mutex
	// slug and convOwner are the app the conversion answers.
	slug, convOwner string
	// convStatus, when not 0, is the conversion's status instead of 201.
	convStatus int
	// installedAt is the lookup that first finds the installation; 0 never.
	installedAt int
	// lookupStatus, when not 0, answers every lookup.
	lookupStatus int
	// statuses, when set, answer the lookups in turn, 200 with the
	// installation; once they run out, the fields above answer.
	statuses []int
	// selection is the installation's repository_selection.
	selection string
	// tokenStatus, when not 0, is the token call's status instead of 201.
	tokenStatus int
	// clock, when set, is GitHub's clock: a JWT expired by it gets 401.
	clock func() time.Time

	conversions, lookups, tokens int
}

func (g *fakeGitHub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /app-manifests/{code}/conversions", g.convert)
	mux.HandleFunc("GET /repos/{owner}/{repo}/installation", g.lookup)
	mux.HandleFunc("POST /app/installations/99/access_tokens", g.token)
	return mux
}

func (g *fakeGitHub) convert(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.conversions++
	switch {
	case g.convStatus != 0:
		reply(w, g.convStatus, `{"message":"Validation Failed"}`)
		return
	case r.PathValue("code") != testCode:
		reply(w, http.StatusNotFound, `{"message":"Not Found"}`)
		return
	}
	body, err := json.Marshal(map[string]any{
		"id": 7, "slug": g.slug, "client_id": "Iv23client", "name": g.slug,
		"html_url": "https://github.test/apps/" + g.slug, "created_at": "2026-10-03T12:00:00Z",
		"owner": map[string]any{"login": g.convOwner, "id": 42}, "pem": string(pkcs1Key()),
	})
	if err != nil {
		reply(w, http.StatusInternalServerError, `{"message":"the fake's reply"}`)
		return
	}
	reply(w, http.StatusCreated, string(body))
}

func (g *fakeGitHub) lookup(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lookups++
	switch {
	case g.clock != nil && expiry(r).Before(g.clock()):
		reply(w, http.StatusUnauthorized, `{"message":"'Expiration time' claim ('exp') is too far in the past"}`)
	case g.lookups <= len(g.statuses) && g.statuses[g.lookups-1] == http.StatusOK:
		reply(w, http.StatusOK, `{"id":99,"repository_selection":"`+g.selection+`"}`)
	case g.lookups <= len(g.statuses):
		status := g.statuses[g.lookups-1]
		reply(w, status, `{"message":"`+http.StatusText(status)+`"}`)
	case g.lookupStatus != 0:
		reply(w, g.lookupStatus, `{"message":"A JSON web token could not be decoded"}`)
	case g.installedAt == 0 || g.lookups < g.installedAt:
		reply(w, http.StatusNotFound, `{"message":"Not Found"}`)
	default:
		reply(w, http.StatusOK, `{"id":99,"repository_selection":"`+g.selection+`"}`)
	}
}

func (g *fakeGitHub) token(w http.ResponseWriter, _ *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tokens++
	if g.tokenStatus != 0 {
		reply(w, g.tokenStatus, `{"message":"There is at least one repository that does not exist"}`)
		return
	}
	reply(w, http.StatusCreated, `{"token":"ghs_secret","expires_at":"2026-10-03T13:00:00Z"}`)
}

// counts returns how many conversions, lookups and token calls were made.
func (g *fakeGitHub) counts() (int, int, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.conversions, g.lookups, g.tokens
}

// expiry returns the exp of r's bearer JWT, or the zero time when it has
// none. It runs in the server's goroutine, so it cannot fail the test.
func expiry(r *http.Request) time.Time {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Exp int64 `json:"exp"`
	}
	_ = json.Unmarshal(raw, &claims)
	return time.Unix(claims.Exp, 0)
}

// page is the loopback page as the browser loaded it.
type page struct {
	// base is the loopback server's URL, http://127.0.0.1:<port>.
	base string
	// action is where the form posts, state the state in it.
	action, state string
	// manifest is the form's manifest field.
	manifest Manifest
}

// response is what the loopback server answered.
type response struct {
	status   int
	location string
	body     string
}

// fakeBrowser plays your browser, and GitHub's pages in it: opening
// the loopback URL loads the page, and then visit runs, which by default
// sends the redirect GitHub sends once you created the app.
type fakeBrowser struct {
	t      *testing.T
	client *http.Client
	// openErr is what opening returns, after you went to the URL anyway.
	openErr error
	// visit is what happens once the loopback page is loaded; nil confirms
	// the creation.
	visit func(p page)

	opened    []string
	pages     []page
	redirects []response
}

func newBrowser(t *testing.T) *fakeBrowser {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(client.CloseIdleConnections)
	return &fakeBrowser{t: t, client: client}
}

// open is the flow's browser opener.
func (b *fakeBrowser) open(u string) error {
	b.opened = append(b.opened, u)
	if base, ok := strings.CutSuffix(u, "/"); ok && strings.HasPrefix(u, "http://127.0.0.1:") {
		p := b.load(base)
		b.pages = append(b.pages, p)
		if b.visit != nil {
			b.visit(p)
		} else {
			b.confirm(p)
		}
	}
	return b.openErr
}

var (
	actionAttr   = regexp.MustCompile(`<form method="post" action="([^"]*)"`)
	manifestAttr = regexp.MustCompile(`<input type="hidden" name="manifest" value="([^"]*)"`)
)

// load gets the loopback page at base and reads its form.
func (b *fakeBrowser) load(base string) page {
	b.t.Helper()
	got := b.get(base + "/")
	action, manifest := actionAttr.FindStringSubmatch(got.body), manifestAttr.FindStringSubmatch(got.body)
	if got.status != http.StatusOK || action == nil || manifest == nil {
		b.t.Fatalf("the loopback page answered %d without the form:\n%s", got.status, got.body)
	}
	p := page{base: base, action: html.UnescapeString(action[1])}
	if err := json.Unmarshal([]byte(html.UnescapeString(manifest[1])), &p.manifest); err != nil {
		b.t.Fatalf("the form's manifest is not JSON: %v", err)
	}
	parsed, err := url.Parse(p.action)
	if err != nil {
		b.t.Fatalf("the form's action %q: %v", p.action, err)
	}
	p.state = parsed.Query().Get("state")
	return p
}

// confirm sends the redirect GitHub sends once you created the app.
func (b *fakeBrowser) confirm(p page) {
	b.t.Helper()
	got := b.created(p, url.Values{"code": {testCode}, "state": {p.state}})
	b.redirects = append(b.redirects, got)
}

// created sends GitHub's redirect to /created with query.
func (b *fakeBrowser) created(p page, query url.Values) response {
	b.t.Helper()
	return b.get(p.base + "/created?" + query.Encode())
}

// get gets u, without following a redirect.
func (b *fakeBrowser) get(u string) response {
	b.t.Helper()
	got, err := b.do(u, "")
	if err != nil {
		b.t.Fatal(err)
	}
	return got
}

// do gets u, without following a redirect, with host as its Host header
// unless host is empty. It does not fail the test, so any goroutine may
// call it.
func (b *fakeBrowser) do(u, host string) (response, error) {
	req, err := http.NewRequestWithContext(b.t.Context(), http.MethodGet, u, nil)
	if err != nil {
		return response{}, fmt.Errorf("GET %s: %w", u, err)
	}
	if host != "" {
		req.Host = host
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("GET %s: %w", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return response{status: resp.StatusCode, location: resp.Header.Get("Location"), body: string(body)}, nil
}

// flowRun is one create flow with every dependency faked.
type flowRun struct {
	api     *fakeGitHub
	client  *Client
	browser *fakeBrowser
	store   *Store
	flow    *Flow
	ghCalls *[]proc.Command
	out     *bytes.Buffer
	ctx     context.Context //nolint:containedctx // the context a test's browser can cancel mid-run
}

// newRun returns a run in a repository crew owned by owner, an organization
// when org is set, where the bot crew-tester is installed at the first
// lookup.
func newRun(t *testing.T, owner string, org bool) *flowRun {
	t.Helper()
	api := &fakeGitHub{slug: "crew-tester", convOwner: owner, installedAt: 1, selection: "selected"}
	srv := httptest.NewServer(api.handler())
	t.Cleanup(srv.Close)
	client := NewClient(srv.URL, srv.Client())
	ownerType := "User"
	if org {
		ownerType = "Organization"
	}
	gh, calls := scriptedGh(fmt.Sprintf(`{"name":"crew","owner":{"login":%q,"id":42,"type":%q}}`, owner, ownerType), nil)
	browser := newBrowser(t)
	r := &flowRun{
		api: api, client: client, browser: browser, store: NewStore(t.TempDir()),
		ghCalls: calls, out: &bytes.Buffer{}, ctx: context.Background(),
	}
	r.flow = NewFlow(gh, r.store, client, r.out, r.out)
	r.flow.Open, r.flow.Web = browser.open, testWeb
	r.flow.CreateTimeout, r.flow.InstallTimeout, r.flow.PollInterval = 10*time.Second, 10*time.Second, time.Millisecond
	return r
}

// create runs crew mates create name in the repository and returns its
// error. Whatever the outcome, neither its output nor its error holds the
// bot's private key (R10).
func (r *flowRun) create(t *testing.T, name string) error {
	t.Helper()
	err := r.flow.Create(r.ctx, "/repo", name)
	all := r.out.String()
	if err != nil {
		all += err.Error()
	}
	checkNoKey(t, all)
	return err
}

// checkNoKey fails the test when text holds any line of the test key.
func checkNoKey(t *testing.T, text string) {
	t.Helper()
	for line := range strings.Lines(string(pkcs1Key())) {
		if line = strings.TrimSpace(line); line != "" && strings.Contains(text, line) {
			t.Fatal("the output holds the mate's private key") // never echo it
		}
	}
}

// lastLine returns the output's last line.
func (r *flowRun) lastLine() string {
	lines := strings.Split(strings.TrimSpace(r.out.String()), "\n")
	return lines[len(lines)-1]
}

// closed fails the test when the loopback server at base still accepts
// connections.
func closed(t *testing.T, base string) {
	t.Helper()
	conn, err := new(net.Dialer).DialContext(t.Context(), "tcp", strings.TrimPrefix(base, "http://"))
	if err == nil {
		_ = conn.Close()
		t.Errorf("the loopback server at %s still listens", base)
	}
}
