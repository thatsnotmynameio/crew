package upgrade

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestReleaseFindsTheLatestReleaseWithTheToken(t *testing.T) {
	rel := v050()
	f := &fakeGitHub{t: t, private: true, latest: rel, releases: []*fakeRelease{rel}}
	got, err := f.serve(testToken).Release(t.Context(), "")
	if err != nil {
		t.Fatalf("Release = %v", err)
	}
	if got.Tag != "v0.5.0" || len(got.Assets) != 2 {
		t.Errorf("Release = %+v, want v0.5.0 with two assets", got)
	}
	for _, r := range f.seen() {
		checkRequest(t, r)
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Errorf("Authorization = %q, want the bearer token", r.Header.Get("Authorization"))
		}
	}
}

func TestReleaseFindsANamedReleaseByItsTag(t *testing.T) {
	old := &fakeRelease{tag: "v0.4.0", assets: map[string][]byte{"checksums.txt": nil}}
	f := &fakeGitHub{t: t, latest: v050(), releases: []*fakeRelease{v050(), old}}
	got, err := f.serve("").Release(t.Context(), "v0.4.0")
	if err != nil || got.Tag != "v0.4.0" {
		t.Fatalf("Release(v0.4.0) = %+v, %v, want v0.4.0", got, err)
	}
	if path := f.seen()[0].URL.Path; path != "/repos/thatsnotmynameio/crew/releases/tags/v0.4.0" {
		t.Errorf("path = %q", path)
	}
}

func TestReleaseEscapesTheTagInItsPath(t *testing.T) {
	f := &fakeGitHub{t: t}
	_, err := f.serve("").Release(t.Context(), "v1/../../x?y")
	if err == nil {
		t.Fatal("Release found a release of a tag that is not one")
	}
	if got := f.seen()[0].URL.RawPath; got != "/repos/thatsnotmynameio/crew/releases/tags/v1%2F..%2F..%2Fx%3Fy" {
		t.Errorf("raw path = %q, want the tag escaped", got)
	}
}

func TestReleaseWithoutATokenSendsNoAuthorization(t *testing.T) {
	rel := v050()
	f := &fakeGitHub{t: t, latest: rel, releases: []*fakeRelease{rel}}
	if _, err := f.serveLogin("", "gh is not on PATH").Release(t.Context(), ""); err != nil {
		t.Fatalf("Release = %v", err)
	}
	for _, r := range f.seen() {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization = %q, want none", auth)
		}
	}
}

// checkError checks that err is an error saying each of want, and not
// saying not when it is not "".
func checkError(t *testing.T, err error, not string, want ...string) {
	t.Helper()
	if err == nil {
		t.Errorf("Release = nil, want an error saying %q", want)
		return
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("Release = %v, want an error saying %q", err, w)
		}
	}
	if not != "" && strings.Contains(err.Error(), not) {
		t.Errorf("Release = %v, want no %q", err, not)
	}
}

func TestReleaseNamesTheCauseOfA404(t *testing.T) {
	tests := []struct {
		name    string
		private bool
		token   string
		tag     string
		want    []string
		not     string
	}{
		{name: "missing tag", tag: "v9.9.9", want: []string{"release v9.9.9 does not exist"}},
		{name: "no release", want: []string{"thatsnotmynameio/crew has no published release"}},
		{name: "private without login", private: true,
			want: []string{"private", "no gh login", "no oauth token found for github.com", "gh auth login"}},
		{name: "private with a login that cannot see it", private: true, token: "gho_other",
			want: []string{"the gh login cannot see thatsnotmynameio/crew"}, not: "gh auth login"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeGitHub{t: t, private: tt.private}
			_, err := f.serveLogin(tt.token, "no oauth token found for github.com").Release(t.Context(), tt.tag)
			checkError(t, err, tt.not, tt.want...)
			if _, ok := errors.AsType[*EnvError](err); ok {
				t.Errorf("Release = %v, want a failure, not an environment error", err)
			}
		})
	}
}

func TestReleaseNamesARejectedLoginARateLimitAndOtherReplies(t *testing.T) {
	reset := time.Date(2026, 10, 8, 21, 49, 0, 0, time.UTC)
	tests := []struct {
		name    string
		token   string
		status  int
		message string
		header  http.Header
		want    string
		not     string
	}{
		{name: "rejected token", token: testToken, status: http.StatusUnauthorized, message: "Bad credentials",
			want: "GitHub rejected the gh login; run gh auth login"},
		{name: "401 without a token", status: http.StatusUnauthorized, message: "Requires authentication",
			want: "401 Unauthorized: Requires authentication"},
		{name: "rate limit without a token", status: http.StatusForbidden, message: "API rate limit exceeded",
			header: http.Header{"X-Ratelimit-Remaining": {"0"}}, want: "rate limit; a gh login raises it: gh auth login"},
		{name: "rate limit with a token", token: testToken, status: http.StatusForbidden,
			header: http.Header{
				"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {strconv.FormatInt(reset.Unix(), 10)},
			},
			want: "rate limit; it resets at 2026-10-08 21:49:00 UTC", not: "gh login"},
		{name: "secondary rate limit with a token", token: testToken, status: http.StatusTooManyRequests,
			header: http.Header{"Retry-After": {"60"}}, want: "rate limit", not: "gh login"},
		{name: "secondary rate limit without a token", status: http.StatusTooManyRequests,
			want: "a gh login raises it"},
		{name: "forbidden", token: testToken, status: http.StatusForbidden, message: "Resource protected by SSO",
			want: "403 Forbidden: Resource protected by SSO"},
		{name: "server error", status: http.StatusBadGateway, message: "Bad Gateway", want: "502 Bad Gateway"},
		{name: "no message", status: http.StatusServiceUnavailable, want: "GitHub answered 503 Service Unavailable"},
		{name: "control characters", status: http.StatusBadGateway, message: "evil\x1b[2Jtext", want: "evil[2Jtext"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeGitHub{t: t, status: tt.status, message: tt.message, header: tt.header}
			_, err := f.serve(tt.token).Release(t.Context(), "")
			checkError(t, err, tt.not, tt.want)
			if err != nil && strings.ContainsRune(err.Error(), '\x1b') {
				t.Errorf("Release = %q, want no control characters", err)
			}
		})
	}
}

func TestReleaseRefusesAReleaseThatIsNotTheOneAsked(t *testing.T) {
	tests := []struct {
		name string
		rel  *fakeRelease
		tag  string
		want string
	}{
		{name: "another tag", rel: &fakeRelease{tag: "v0.5.0"}, tag: "v0.4.0", want: "v0.5.0"},
		{name: "unparsable tag", rel: &fakeRelease{tag: "nightly"}, want: "nightly"},
		{name: "tag without v", rel: &fakeRelease{tag: "0.5.0"}, want: "0.5.0"},
		{name: "draft", rel: &fakeRelease{tag: "v0.5.0", draft: true}, want: "not a published release"},
		{name: "prerelease", rel: &fakeRelease{tag: "v0.5.0", prerelease: true}, want: "not a published release"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A GitHub that answers rel to every lookup.
			f := &fakeGitHub{t: t, releases: []*fakeRelease{tt.rel}}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				f.answerRelease(w, tt.rel)
			}))
			t.Cleanup(srv.Close)
			_, err := NewClient(API{Base: srv.URL}, srv.Client(), noToken).Release(t.Context(), tt.tag)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Release = %v, want an error saying %q", err, tt.want)
			}
		})
	}
}

func TestReleaseRefusesAnUnreadableReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, `{"tag_name":`)
	}))
	t.Cleanup(srv.Close)
	_, err := NewClient(API{Base: srv.URL}, srv.Client(), noToken).Release(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "unreadable") {
		t.Errorf("Release = %v, want an unreadable reply", err)
	}
}

func TestReleaseSaysWhenGitHubCannotBeReached(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	_, err := NewClient(API{Base: base}, http.DefaultClient, noToken).Release(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "could not reach GitHub") {
		t.Errorf("Release = %v, want could not reach GitHub", err)
	}
	if errors.Is(err, ErrInterrupted) {
		t.Errorf("Release = %v, want no interrupt", err)
	}
}

func TestReleaseRefusesAnOversizedReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reply(w, http.StatusOK, `{"tag_name":"v0.5.0","body":"`+strings.Repeat("a", maxJSON)+`"}`)
	}))
	t.Cleanup(srv.Close)
	_, err := NewClient(API{Base: srv.URL}, srv.Client(), noToken).Release(t.Context(), "")
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Errorf("Release = %v, want a too-large error", err)
	}
}

func TestReleaseInterruptedIsNotBlamedOnGitHub(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	_, err := NewClient(API{Base: srv.URL}, srv.Client(), noToken).Release(ctx, "")
	if !errors.Is(err, ErrInterrupted) || strings.Contains(err.Error(), "could not reach GitHub") {
		t.Errorf("Release = %v, want an interrupt that blames nothing", err)
	}
}

func TestDownloadFetchesAnAssetByIdAsBytes(t *testing.T) {
	rel := v050()
	f := &fakeGitHub{t: t, private: true, latest: rel, releases: []*fakeRelease{rel}}
	c := f.serve(testToken)
	release, err := c.Release(t.Context(), "")
	if err != nil {
		t.Fatalf("Release = %v", err)
	}
	data, err := c.Download(t.Context(), release, "crew_linux_amd64.tar.gz", maxArchive)
	if err != nil || string(data) != "archive" {
		t.Fatalf("Download = %q, %v, want the archive", data, err)
	}
	last := f.seen()[len(f.seen())-1]
	id := f.assetID(rel, "crew_linux_amd64.tar.gz")
	if want := "/repos/thatsnotmynameio/crew/releases/assets/" + strconv.FormatInt(id, 10); last.URL.Path != want {
		t.Errorf("asset path = %q, want %q", last.URL.Path, want)
	}
	if last.Header.Get("Accept") != "application/octet-stream" {
		t.Errorf("Accept = %q, want application/octet-stream", last.Header.Get("Accept"))
	}
	if last.Header.Get("Authorization") != "Bearer "+testToken {
		t.Errorf("Authorization = %q, want the bearer token", last.Header.Get("Authorization"))
	}
}

func TestDownloadFailsWithoutTheAssetOrOverItsCap(t *testing.T) {
	rel := v050()
	rel.assets["checksums.txt"] = []byte(strings.Repeat("s", maxSums+1))
	f := &fakeGitHub{t: t, latest: rel, releases: []*fakeRelease{rel}}
	c := f.serve("")
	release, err := c.Release(t.Context(), "")
	if err != nil {
		t.Fatalf("Release = %v", err)
	}
	if _, err := c.Download(t.Context(), release, "crew_darwin_arm64.tar.gz", maxArchive); err == nil ||
		!strings.Contains(err.Error(), "v0.5.0 has no crew_darwin_arm64.tar.gz") {
		t.Errorf("Download of a missing asset = %v", err)
	}
	if _, err := c.Download(t.Context(), release, "crew_linux_amd64.tar.gz", 3); err == nil ||
		!strings.Contains(err.Error(), "crew_linux_amd64.tar.gz is larger than") {
		t.Errorf("Download over the cap = %v", err)
	}
	if _, err := c.Download(t.Context(), release, "checksums.txt", maxSums); err == nil ||
		!strings.Contains(err.Error(), "checksums.txt is larger than") {
		t.Errorf("Download of checksums.txt over its cap = %v", err)
	}
}

func TestDownloadRefusesJSONInsteadOfBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"id":1}`)
	}))
	t.Cleanup(srv.Close)
	c := NewClient(API{Base: srv.URL}, srv.Client(), noToken)
	rel := Release{Tag: "v0.5.0", Assets: []Asset{{ID: 1, Name: "checksums.txt"}}}
	_, err := c.Download(t.Context(), rel, "checksums.txt", maxSums)
	if err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Errorf("Download = %v, want a JSON error", err)
	}
}

func TestDownloadInterruptedIsNotBlamedOnGitHub(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, "part of the archive")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		cancel()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	c := NewClient(API{Base: srv.URL}, srv.Client(), noToken)
	rel := Release{Tag: "v0.5.0", Assets: []Asset{{ID: 1, Name: "a"}}}
	_, err := c.Download(ctx, rel, "a", maxArchive)
	if !errors.Is(err, ErrInterrupted) || strings.Contains(err.Error(), "could not reach GitHub") {
		t.Errorf("Download = %v, want an interrupt that blames nothing", err)
	}
}

// redirectTo returns an API server that redirects every request to target,
// and a client of it with the test gh login.
func redirectTo(t *testing.T, target string) *Client {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}))
	t.Cleanup(api.Close)
	return NewClient(API{Base: api.URL}, api.Client(), withToken)
}

// signedServer returns a server that answers bytes and records the
// Authorization it got.
func signedServer(t *testing.T, auth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, "bytes")
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestARedirectToAnotherHostCarriesNoToken(t *testing.T) {
	var auth string
	signed := signedServer(t, &auth)
	// The signed server is reached by another name than the API.
	signedURL := strings.Replace(signed.URL, "127.0.0.1", "localhost", 1)
	c := redirectTo(t, signedURL+"/signed?sig=secret")
	data, err := c.Download(t.Context(), Release{Tag: "v0.5.0", Assets: []Asset{{ID: 7, Name: "a"}}}, "a", maxArchive)
	if err != nil || string(data) != "bytes" {
		t.Fatalf("Download = %q, %v", data, err)
	}
	if auth != "" {
		t.Errorf("the signed host got Authorization %q, want none", auth)
	}
}

func TestARedirectToAnotherPortOfTheSameHostCarriesNoToken(t *testing.T) {
	var auth string
	signed := signedServer(t, &auth)
	c := redirectTo(t, signed.URL+"/signed?sig=secret")
	data, err := c.Download(t.Context(), Release{Tag: "v0.5.0", Assets: []Asset{{ID: 7, Name: "a"}}}, "a", maxArchive)
	if err != nil || string(data) != "bytes" {
		t.Fatalf("Download = %q, %v", data, err)
	}
	if auth != "" {
		t.Errorf("another port got Authorization %q, want none", auth)
	}
}

func TestARedirectThatCannotConnectNamesNoURL(t *testing.T) {
	l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := "http://" + l.Addr().String()
	_ = l.Close()
	c := redirectTo(t, closed+"/signed?sig=secret")
	_, err = c.Download(t.Context(), Release{Tag: "v0.5.0", Assets: []Asset{{ID: 7, Name: "a"}}}, "a", maxArchive)
	if err == nil || !strings.Contains(err.Error(), "could not reach GitHub") {
		t.Errorf("Download = %v, want could not reach GitHub", err)
	}
	if err != nil && (strings.Contains(err.Error(), "sig=secret") || strings.Contains(err.Error(), "/signed")) {
		t.Errorf("Download = %q, want no redirect URL in the message", err)
	}
}

func TestARedirectFromHTTPSToHTTPIsRefused(t *testing.T) {
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "bytes")
	}))
	t.Cleanup(plain.Close)
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/signed?sig=secret", http.StatusFound)
	}))
	t.Cleanup(api.Close)
	c := NewClient(API{Base: api.URL}, api.Client(), noToken)
	_, err := c.Download(t.Context(), Release{Tag: "v0.5.0", Assets: []Asset{{ID: 7, Name: "a"}}}, "a", maxArchive)
	if err == nil || !strings.Contains(err.Error(), "not https") {
		t.Fatalf("Download = %v, want a refused redirect to http", err)
	}
	if strings.Contains(err.Error(), "sig=secret") {
		t.Errorf("Download = %q, want no redirect URL in the message", err)
	}
}

func TestTooManyRedirectsFail(t *testing.T) {
	var api *httptest.Server
	api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, api.URL+"/again", http.StatusFound)
	}))
	t.Cleanup(api.Close)
	c := NewClient(API{Base: api.URL}, api.Client(), noToken)
	_, err := c.Download(t.Context(), Release{Tag: "v0.5.0", Assets: []Asset{{ID: 7, Name: "a"}}}, "a", maxArchive)
	if err == nil || !strings.Contains(err.Error(), "redirected more than") {
		t.Errorf("Download = %v, want too many redirects", err)
	}
}
