package mates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	// AE2: the boss renamed the app on GitHub's page.
	want := Mate{
		Name: "dev", Owner: "thatsnotmynameio", OwnerID: 42, AppID: 7, ClientID: "Iv23client",
		Slug: "thatsnotmyname-crew-dev", AppName: "thatsnotmyname-crew-dev",
		HTMLURL: "https://github.com/apps/thatsnotmyname-crew-dev", BotLogin: "thatsnotmyname-crew-dev[bot]",
		CreatedAt: testNow, PrivateKey: key,
	}
	if got := conv.Mate("dev"); got != want {
		t.Errorf("Mate = %+v, want %+v", got, want)
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

func TestRepoInstallationSignsAsTheMate(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		checkRequest(t, r, http.MethodGet, "/repos/thatsnotmynameio/crew/installation")
		if iss := bearerClaims(t, r)["iss"]; iss != "Iv23client" {
			t.Errorf("iss = %v, want the mate's client id", iss)
		}
		reply(w, http.StatusOK, `{"id":99,"repository_selection":"selected","account":{"login":"thatsnotmynameio"}}`)
	})
	got, err := c.RepoInstallation(context.Background(), testMate("thatsnotmynameio"), "thatsnotmynameio", "crew")
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
		if _, err := c.RepoInstallation(context.Background(), testMate("thatsnotmynameio"), "o", "r"); err != nil {
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
			_, err := c.RepoInstallation(context.Background(), testMate("thatsnotmynameio"), "o", "r")
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
	m := testMate("thatsnotmynameio")
	m.PrivateKey = "not a key"
	if _, err := c.RepoInstallation(context.Background(), m, "o", "r"); err == nil {
		t.Error("RepoInstallation with a broken key = nil error, want one")
	}
}

func TestAccessTokenIsLimitedToTheRepository(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		checkRequest(t, r, http.MethodPost, "/app/installations/99/access_tokens")
		bearerClaims(t, r)
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"repositories":["crew"]}` {
			t.Errorf("body = %s, want the repository by name", body)
		}
		reply(w, http.StatusCreated, `{"token":"ghs_secret","expires_at":"2026-10-03T13:00:00Z"}`)
	})
	if err := c.AccessToken(context.Background(), testMate("thatsnotmynameio"), 99, "crew"); err != nil {
		t.Errorf("AccessToken: %v", err)
	}
}

func TestAccessTokenFailureCarriesNoRequest(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusUnprocessableEntity, `{"message":"There is at least one repository that does not exist"}`)
	})
	err := c.AccessToken(context.Background(), testMate("thatsnotmynameio"), 99, "crew")
	if err == nil || !strings.Contains(err.Error(), "does not exist") || !strings.Contains(err.Error(), "422") {
		t.Fatalf("AccessToken on 422 = %v, want the status and GitHub's message", err)
	}
	if strings.Contains(err.Error(), "Bearer") || strings.Contains(err.Error(), "repositories") {
		t.Errorf("AccessToken = %v, carries the request", err)
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
		_, err := c.RepoInstallation(context.Background(), testMate(testOwner), testOwner, "crew")
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
