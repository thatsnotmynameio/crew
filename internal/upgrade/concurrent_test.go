package upgrade

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type releaseTransport struct{ t *testing.T }

func (r releaseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if got := req.Header.Get("Authorization"); got != "Bearer "+testToken {
		r.t.Errorf("Authorization = %q, want the bearer token", got)
	}
	body := `{"tag_name":"v0.5.0"}`
	if req.Header.Get("Accept") == "application/octet-stream" {
		body = "bytes"
	}
	return &http.Response{
		StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)),
	}, nil
}

// TestConcurrentCallsShareOneTokenLookup starts releases and downloads on
// one client at once: under -race, a lookup of gh's login that is not
// synchronised is a data race, and every call must send the one token.
func TestConcurrentCallsShareOneTokenLookup(t *testing.T) {
	var calls atomic.Int64
	c := NewClient(API{Base: DefaultAPI}, &http.Client{Transport: releaseTransport{t: t}},
		func(context.Context, API) (string, string) {
			calls.Add(1)
			return testToken, ""
		})
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := range 16 {
		workers.Go(func() {
			<-start
			callConcurrently(t, c, i%2 == 0)
		})
	}
	close(start)
	workers.Wait()
	if got := calls.Load(); got != 1 {
		t.Errorf("token lookups = %d, want one", got)
	}
}

func callConcurrently(t *testing.T, c *Client, release bool) {
	t.Helper()
	if release {
		rel, err := c.Release(t.Context(), "")
		if err != nil || rel.Tag != "v0.5.0" {
			t.Errorf("Release = %+v, %v", rel, err)
		}
		return
	}
	data, err := c.Download(t.Context(), releaseOfA(), "a", maxArchive)
	if err != nil || string(data) != "bytes" {
		t.Errorf("Download = %q, %v", data, err)
	}
}
