package upgrade

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// testToken is the token of the test gh login.
const testToken = "gho_testtoken0123456789"

// fakeRelease is a release the fake GitHub serves.
type fakeRelease struct {
	tag        string
	draft      bool
	prerelease bool
	// assets are its files by name, served with ids in name order.
	assets map[string][]byte
}

// fakeGitHub serves the releases of thatsnotmynameio/crew as GitHub's REST
// API does, and records the requests it got.
type fakeGitHub struct {
	t       *testing.T
	private bool
	// latest is the release /releases/latest answers, none when nil.
	latest   *fakeRelease
	releases []*fakeRelease
	// status, when not zero, answers every request with it and message.
	status  int
	message string
	header  http.Header

	mu       sync.Mutex
	requests []*http.Request
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.mu.Unlock()
	if f.status != 0 {
		maps.Copy(w.Header(), f.header)
		message, err := json.Marshal(f.message)
		if err != nil {
			f.t.Errorf("encoding %q: %v", f.message, err)
		}
		reply(w, f.status, `{"message":`+string(message)+`}`)
		return
	}
	if f.private && r.Header.Get("Authorization") != "Bearer "+testToken {
		reply(w, http.StatusNotFound, `{"message":"Not Found"}`)
		return
	}
	const prefix = "/repos/thatsnotmynameio/crew"
	path := r.URL.Path
	switch {
	case path == prefix:
		reply(w, http.StatusOK, `{"full_name":"thatsnotmynameio/crew"}`)
	case path == prefix+"/releases/latest":
		f.answerRelease(w, f.latest)
	case strings.HasPrefix(path, prefix+"/releases/tags/"):
		f.answerRelease(w, f.byTag(strings.TrimPrefix(path, prefix+"/releases/tags/")))
	case strings.HasPrefix(path, prefix+"/releases/assets/"):
		f.answerAsset(w, r, strings.TrimPrefix(path, prefix+"/releases/assets/"))
	default:
		reply(w, http.StatusNotFound, `{"message":"Not Found"}`)
	}
}

// serve starts the fake and returns a client of it whose gh login is
// token.
func (f *fakeGitHub) serve(token string) *Client {
	return f.serveLogin(token, "")
}

// serveLogin starts the fake and returns a client of it whose token
// function answers token, or no token and reason.
func (f *fakeGitHub) serveLogin(token, reason string) *Client {
	f.t.Helper()
	srv := httptest.NewServer(f)
	f.t.Cleanup(srv.Close)
	api := API{Base: srv.URL, Host: "127.0.0.1"}
	return NewClient(api, srv.Client(), func(context.Context, API) (string, string) { return token, reason })
}

// seen returns the requests the fake got.
func (f *fakeGitHub) seen() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

func (f *fakeGitHub) byTag(tag string) *fakeRelease {
	for _, rel := range f.releases {
		if rel.tag == tag {
			return rel
		}
	}
	return nil
}

// assetID is the id of a release's asset: its release's index times 100,
// plus its index in name order, plus one.
func (f *fakeGitHub) assetID(rel *fakeRelease, name string) int64 {
	names := slices.Sorted(maps.Keys(rel.assets))
	return int64(slices.Index(f.releases, rel)*100 + slices.Index(names, name) + 1)
}

func (f *fakeGitHub) answerRelease(w http.ResponseWriter, rel *fakeRelease) {
	if rel == nil {
		reply(w, http.StatusNotFound, `{"message":"Not Found"}`)
		return
	}
	type asset struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	assets := []asset{}
	for name := range rel.assets {
		// The JSON's url points elsewhere: crew must build its own.
		assets = append(assets, asset{ID: f.assetID(rel, name), Name: name, URL: "https://evil.example/asset"})
	}
	body, err := json.Marshal(struct {
		Tag        string  `json:"tag_name"`
		Draft      bool    `json:"draft"`
		Prerelease bool    `json:"prerelease"`
		Assets     []asset `json:"assets"`
	}{rel.tag, rel.draft, rel.prerelease, assets})
	if err != nil {
		f.t.Errorf("encoding release %s: %v", rel.tag, err)
	}
	reply(w, http.StatusOK, string(body))
}

func (f *fakeGitHub) answerAsset(w http.ResponseWriter, r *http.Request, id string) {
	for _, rel := range f.releases {
		for name, data := range rel.assets {
			if strconv.FormatInt(f.assetID(rel, name), 10) != id {
				continue
			}
			if r.Header.Get("Accept") != "application/octet-stream" {
				reply(w, http.StatusOK, `{"name":"`+name+`"}`)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(data)
			return
		}
	}
	reply(w, http.StatusNotFound, `{"message":"Not Found"}`)
}

// reply answers with status and a JSON body.
func reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// checkRequest checks that r carries the headers every JSON call sends.
func checkRequest(t *testing.T, r *http.Request) {
	t.Helper()
	for header, want := range map[string]string{
		"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28",
	} {
		if got := r.Header.Get(header); got != want {
			t.Errorf("%s %s: %s = %q, want %q", r.Method, r.URL.Path, header, got, want)
		}
	}
	if r.Header.Get("User-Agent") == "" {
		t.Errorf("%s %s: no User-Agent", r.Method, r.URL.Path)
	}
}

// noToken is a token function for no gh login.
func noToken(context.Context, API) (string, string) { return "", "gh is not on PATH" }

// withToken is a token function for the test gh login.
func withToken(context.Context, API) (string, string) { return testToken, "" }

func v050() *fakeRelease {
	return &fakeRelease{tag: "v0.5.0", assets: map[string][]byte{
		"checksums.txt": []byte("sums"), "crew_linux_amd64.tar.gz": []byte("archive"),
	}}
}
