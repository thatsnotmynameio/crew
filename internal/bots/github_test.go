package bots

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

// testNow is the time the test clients mint their JWTs at.
var testNow = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

// testClient returns a client of a test server running handler, minting
// its JWTs at testNow.
func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(srv.URL, srv.Client())
	c.now = func() time.Time { return testNow }
	return c
}

// checkRequest checks that r is a GitHub API request for method and path,
// with the headers every request sends.
func checkRequest(t *testing.T, r *http.Request, method, path string) {
	t.Helper()
	if r.Method != method || r.URL.Path != path {
		t.Errorf("request %s %s, want %s %s", r.Method, r.URL.Path, method, path)
	}
	for header, want := range map[string]string{
		"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28",
	} {
		if got := r.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	if r.Header.Get("User-Agent") == "" {
		t.Error("no User-Agent")
	}
}

// bearerClaims checks that r carries an app JWT signed with the test key and
// returns its claims.
func bearerClaims(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		t.Fatalf("Authorization = %q, want a Bearer token", r.Header.Get("Authorization"))
	}
	_, claims := verifyJWT(t, token)
	return claims
}

// reply answers with status and body.
func reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestConvertExchangesTheCodeWithoutAuthentication(t *testing.T) {
	key := pkcs1Key()
	body, err := json.Marshal(map[string]any{
		"id": 7, "slug": "thatsnotmyname-crew-dev", "client_id": "Iv23client", "name": "thatsnotmyname-crew-dev",
		"html_url": "https://github.com/apps/thatsnotmyname-crew-dev", "created_at": "2026-10-03T12:00:00Z",
		"owner": map[string]any{"login": "thatsnotmynameio", "id": 42, "type": "Organization"},
		"pem":   string(key), "client_secret": "shh", "webhook_secret": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		checkRequest(t, r, http.MethodPost, "/app-manifests/code123/conversions")
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want none", got)
		}
		reply(w, http.StatusCreated, string(body))
	})
	conv, err := c.Convert(context.Background(), "code123")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	// AE2: you renamed the app on GitHub's page.
	want := Bot{
		Name: "dev", Owner: "thatsnotmynameio", OwnerID: 42, AppID: 7, ClientID: "Iv23client",
		Slug: "thatsnotmyname-crew-dev", AppName: "thatsnotmyname-crew-dev",
		HTMLURL: "https://github.com/apps/thatsnotmyname-crew-dev", BotLogin: "thatsnotmyname-crew-dev[bot]",
		CreatedAt: testNow, PrivateKey: key,
	}
	if got := conv.Bot("dev"); got != want {
		t.Errorf("Bot = %+v, want %+v", got, want)
	}
}

func TestConvertFailureCarriesGitHubsMessage(t *testing.T) {
	for status, message := range map[int]string{
		http.StatusNotFound:            "Not Found",
		http.StatusUnprocessableEntity: "Validation Failed",
		http.StatusBadGateway:          "",
	} {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			reply(w, status, `{"message":"`+message+`"}`)
		})
		_, err := c.Convert(context.Background(), "code123")
		if err == nil || !strings.Contains(err.Error(), message) || !strings.Contains(err.Error(), http.StatusText(status)) {
			t.Errorf("Convert on %d = %v, want the status and %q", status, err, message)
		}
	}
}

func TestRepoInstallationSignsAsTheBot(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		checkRequest(t, r, http.MethodGet, "/repos/thatsnotmynameio/crew/installation")
		if iss := bearerClaims(t, r)["iss"]; iss != "Iv23client" {
			t.Errorf("iss = %v, want the mate's client id", iss)
		}
		reply(w, http.StatusOK, `{"id":99,"repository_selection":"selected","account":{"login":"thatsnotmynameio"}}`)
	})
	got, err := c.RepoInstallation(context.Background(), testBot("thatsnotmynameio"), "thatsnotmynameio", "crew")
	if err != nil {
		t.Fatalf("RepoInstallation: %v", err)
	}
	if want := (Installation{ID: 99, RepositorySelection: "selected"}); got != want {
		t.Errorf("RepoInstallation = %+v, want %+v", got, want)
	}
}

func TestRepoInstallationMintsAFreshJWTForEveryRequest(t *testing.T) {
	var iats []float64
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		iat, _ := bearerClaims(t, r)["iat"].(float64)
		iats = append(iats, iat)
		reply(w, http.StatusOK, `{"id":99,"repository_selection":"all"}`)
	})
	now := testNow
	c.now = func() time.Time { return now }
	for range 2 {
		if _, err := c.RepoInstallation(context.Background(), testBot("thatsnotmynameio"), "o", "r"); err != nil {
			t.Fatalf("RepoInstallation: %v", err)
		}
		now = now.Add(10 * time.Minute)
	}
	if len(iats) != 2 || int64(iats[1]-iats[0]) != 600 {
		t.Errorf("iats = %v, want two JWTs 10 minutes apart", iats)
	}
}

func TestRepoInstallationTellsNotInstalledFromKeyRejected(t *testing.T) {
	for name, tc := range map[string]struct {
		status              int
		notInstalled, wrong bool
	}{
		"404 not installed": {status: http.StatusNotFound, notInstalled: true},
		"401 key rejected":  {status: http.StatusUnauthorized, wrong: true},
		"500 other":         {status: http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				reply(w, tc.status, `{"message":"nope"}`)
			})
			_, err := c.RepoInstallation(context.Background(), testBot("thatsnotmynameio"), "o", "r")
			if err == nil {
				t.Fatal("RepoInstallation = nil error, want one")
			}
			if errors.Is(err, ErrNotInstalled) != tc.notInstalled || errors.Is(err, ErrKeyRejected) != tc.wrong {
				t.Errorf("RepoInstallation = %v, want not installed %v and key rejected %v", err, tc.notInstalled, tc.wrong)
			}
		})
	}
}

func TestRequestsWithAnUnusableKeyAreNotSent(t *testing.T) {
	c := testClient(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("sent a request without a JWT")
	})
	m := testBot("thatsnotmynameio")
	m.PrivateKey = "not a key"
	if _, err := c.RepoInstallation(context.Background(), m, "o", "r"); err == nil {
		t.Error("RepoInstallation with a broken key = nil error, want one")
	}
}

// testGrant is the reply of a token call granting what crew asks for.
const testGrant = `{"token":"ghs_secret","expires_at":"2026-10-03T13:00:00Z","permissions":{"issues":"write",` +
	`"pull_requests":"write","contents":"read","checks":"read","statuses":"read","actions":"read","metadata":"read"}}`

func TestAccessTokenIsLimitedToTheRepositoryAndCrewsPermissions(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		checkRequest(t, r, http.MethodPost, "/app/installations/99/access_tokens")
		bearerClaims(t, r)
		var body struct {
			Repositories []string          `json:"repositories"`
			Permissions  map[string]string `json:"permissions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("body: %v", err)
		}
		if !slices.Equal(body.Repositories, []string{"crew"}) || !maps.Equal(body.Permissions, permissions()) {
			t.Errorf("body = %+v, want the repository by name and the manifest's permissions", body)
		}
		reply(w, http.StatusCreated, testGrant)
	})
	got, err := c.AccessToken(context.Background(), testBot("thatsnotmynameio"), 99, "crew")
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got.Token != "ghs_secret" || !got.ExpiresAt.Equal(testNow.Add(time.Hour)) ||
		!maps.Equal(got.Permissions, permissions()) {
		t.Errorf("AccessToken = %+v, want the token, its expiry and its permissions", got)
	}
}

func TestPermissionsAreTheManifests(t *testing.T) {
	if got := NewManifest("tester", "o", "http://x").Permissions; !maps.Equal(got, permissions()) {
		t.Errorf("manifest permissions = %v, want %v", got, permissions())
	}
	p := permissions()
	p["contents"] = permWrite
	if permissions()["contents"] != permRead {
		t.Error("changing Permissions' map changed the next one")
	}
}

func TestTokenNeverPrints(t *testing.T) {
	tok := Token("ghs_secret")
	g := Grant{Token: tok}
	for _, s := range []string{
		fmt.Sprint(tok), fmt.Sprintf("%s %v %q %d %x %#v", tok, tok, tok, tok, tok, tok), tok.String(),
		fmt.Sprintf("%+v %#v", g, g),
	} {
		if strings.Contains(s, "ghs_secret") {
			t.Errorf("%q prints the token", s)
		}
	}
}

func TestAccessTokenFailureCarriesNoRequest(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusUnprocessableEntity, `{"message":"There is at least one repository that does not exist"}`)
	})
	_, err := c.AccessToken(context.Background(), testBot("thatsnotmynameio"), 99, "crew")
	if err == nil || !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), "422") {
		t.Fatalf("AccessToken on 422 = %v, want the status and GitHub's message", err)
	}
	if strings.Contains(err.Error(), "Bearer") || strings.Contains(err.Error(), "repositories") {
		t.Errorf("AccessToken = %v, carries the request", err)
	}
}

func TestBotUserIDAsksWithTheInstallationToken(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		checkRequest(t, r, http.MethodGet, "/users/crew-ops[bot]")
		if r.RequestURI != "/users/crew-ops%5Bbot%5D" {
			t.Errorf("request URI = %q, want the brackets escaped", r.RequestURI)
		}
		if got := r.Header.Get("Authorization"); got != "token ghs_secret" {
			t.Errorf("Authorization = %q, want the installation token", got)
		}
		reply(w, http.StatusOK, `{"login":"crew-ops[bot]","id":123,"type":"Bot"}`)
	})
	id, err := c.BotUserID(context.Background(), "ghs_secret", "crew-ops")
	if err != nil || id != 123 {
		t.Errorf("BotUserID = %d, %v; want 123", id, err)
	}
}

func TestBotUserIDRefusesWhatCannotBeTrusted(t *testing.T) {
	for name, tc := range map[string]struct {
		slug, body string
		status     int
	}{
		"slug with a quote": {slug: "crew-'ops", body: `{"id":123}`, status: http.StatusOK},
		"slug in capitals":  {slug: "Crew-ops", body: `{"id":123}`, status: http.StatusOK},
		"empty slug":        {slug: "", body: `{"id":123}`, status: http.StatusOK},
		"non-numeric id":    {slug: "crew-ops", body: `{"id":"123; rm -rf /"}`, status: http.StatusOK},
		"zero id":           {slug: "crew-ops", body: `{"id":0}`, status: http.StatusOK},
		"negative id":       {slug: "crew-ops", body: `{"id":-4}`, status: http.StatusOK},
		"not found":         {slug: "crew-ops", body: `{"message":"Not Found"}`, status: http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
				reply(w, tc.status, tc.body)
			})
			id, err := c.BotUserID(context.Background(), "ghs_secret", tc.slug)
			if err == nil {
				t.Errorf("BotUserID = %d, nil error; want an error", id)
			}
			if err != nil && strings.Contains(err.Error(), "ghs_secret") {
				t.Errorf("BotUserID = %v, quotes the token", err)
			}
		})
	}
}

func TestATokenRequestIsNotAKeyRejection(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusUnauthorized, `{"message":"Bad credentials"}`)
	})
	_, err := c.BotUserID(context.Background(), "ghs_secret", "crew-ops")
	if err == nil || errors.Is(err, ErrKeyRejected) {
		t.Errorf("BotUserID on 401 = %v, want an error that is not ErrKeyRejected", err)
	}
}

func TestRequestsTimeOut(t *testing.T) {
	c := testClient(t, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	c.timeout = 10 * time.Millisecond
	if _, err := c.Convert(context.Background(), "code123"); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Convert on a silent server = %v, want a deadline error", err)
	}
}

func TestTransientTellsTheErrorsARetryMayPass(t *testing.T) {
	limited := func(header, value, message string) error {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			if header != "" {
				w.Header().Set(header, value)
			}
			reply(w, http.StatusForbidden, `{"message":"`+message+`"}`)
		})
		_, err := c.RepoInstallation(context.Background(), testBot(testOwner), testOwner, "crew")
		return err
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "no reply", err: fmt.Errorf("GitHub API: %w", &url.Error{Op: "Get", Err: io.ErrUnexpectedEOF}), want: true},
		{name: "server error", err: &statusError{code: http.StatusBadGateway}, want: true},
		{name: "too many requests", err: &statusError{code: http.StatusTooManyRequests}, want: true},
		{name: "rate limit remaining 0", err: limited("X-RateLimit-Remaining", "0", "API rate limit exceeded"), want: true},
		{name: "retry after", err: limited("Retry-After", "60", "slow down"), want: true},
		{name: "secondary rate limit", err: limited("", "", "You have exceeded a secondary rate limit"), want: true},
		{name: "forbidden", err: limited("", "", "Resource not accessible by integration"), want: false},
		{name: "unprocessable", err: &statusError{code: http.StatusUnprocessableEntity}, want: false},
		{name: "key rejected", err: fmt.Errorf("%w: %w", ErrKeyRejected, &statusError{code: 401}), want: false},
		{name: "unreadable reply", err: errors.New("GitHub API: unreadable reply: EOF"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := transient(tt.err); got != tt.want {
				t.Errorf("transient(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
