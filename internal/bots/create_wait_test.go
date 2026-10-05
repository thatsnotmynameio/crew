package bots

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// lateConversion holds GitHub's reply to the conversion until the flow
// stopped its loopback server, so the exchange is still in flight when the
// wait ends, as when Ctrl-C or the timeout comes while GitHub answers. Then
// it lets the reply through even though the flow cancelled the request, or,
// when fail is set, fails with the cancellation.
type lateConversion struct {
	t    *testing.T
	next http.RoundTripper
	// arrived is closed once the conversion is in flight; then before runs,
	// such as the test's cancel.
	arrived chan struct{}
	before  func()
	// base receives the loopback server's URL.
	base chan string
	fail bool
}

func (l *lateConversion) RoundTrip(req *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(req.URL.Path, "/conversions") {
		return roundTrip(l.next, req)
	}
	close(l.arrived)
	l.before()
	ctx := context.WithoutCancel(req.Context())
	waitClosed(ctx, l.t, <-l.base)
	if l.fail {
		return nil, fmt.Errorf("the conversion: %w", req.Context().Err())
	}
	return roundTrip(l.next, req.WithContext(ctx))
}

// roundTrip sends req through next.
func roundTrip(next http.RoundTripper, req *http.Request) (*http.Response, error) {
	resp, err := next.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, err)
	}
	return resp, nil
}

// waitClosed returns once the loopback server at base refuses connections.
// It runs outside the test's goroutine, so it reports with Errorf.
func waitClosed(ctx context.Context, t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := new(net.Dialer).DialContext(ctx, "tcp", strings.TrimPrefix(base, "http://"))
		if err != nil {
			return
		}
		_ = conn.Close()
		time.Sleep(time.Millisecond)
	}
	t.Errorf("the loopback server at %s never stopped", base)
}

// lateRun returns a run whose GitHub redirect is still being handled when
// the wait ends, and a channel that receives the browser's answer to it.
func lateRun(t *testing.T, fail bool) (*flowRun, *lateConversion, chan response) {
	t.Helper()
	r := newRun(t, testOwner, true)
	late := &lateConversion{
		t: t, next: r.client.http.Transport, arrived: make(chan struct{}), before: func() {},
		base: make(chan string, 1), fail: fail,
	}
	r.client.http = &http.Client{Transport: late}
	answered := make(chan response, 1)
	r.browser.visit = func(p page) {
		late.base <- p.base
		go func() {
			got, err := r.browser.do(p.base+"/created?"+url.Values{"code": {testCode}, "state": {p.state}}.Encode(), "")
			if err != nil {
				t.Errorf("GitHub's redirect: %v", err)
			}
			answered <- got
		}()
		<-late.arrived // the wait starts with the exchange in flight
	}
	return r, late, answered
}

// stopsMidExchange returns the two ways a wait ends while GitHub's redirect
// is being handled.
func stopsMidExchange() map[string]func(r *flowRun, late *lateConversion) {
	return map[string]func(r *flowRun, late *lateConversion){
		"timeout": func(r *flowRun, _ *lateConversion) { r.flow.CreateTimeout = time.Millisecond },
		"stop signal": func(r *flowRun, late *lateConversion) {
			ctx, cancel := context.WithCancel(context.Background())
			r.ctx, late.before = ctx, cancel
		},
	}
}

// checkSavedLate fails the test unless err is the runtime failure of a wait
// that ended after the run saved the bot, and says so.
func checkSavedLate(t *testing.T, r *flowRun, err error) {
	t.Helper()
	if err == nil || isEnvError(err) {
		t.Fatalf("Create = %v, want a runtime failure", err)
	}
	if strings.Contains(err.Error(), "saved nothing") {
		t.Errorf("Create = %v, but the mate is saved", err)
	}
	for _, want := range []string{r.store.Path(testOwner, "tester"), "crew mates create tester again installs it"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Create = %v, want it to say %q", err, want)
		}
	}
}

func TestAStopDuringTheExchangeReportsTheSavedBot(t *testing.T) {
	for name, stop := range stopsMidExchange() {
		t.Run(name, func(t *testing.T) {
			r, late, answered := lateRun(t, false)
			stop(r, late)
			err := r.create(t, "tester")
			if name == "stop signal" && !errors.Is(err, context.Canceled) {
				t.Errorf("Create = %v, want it to wrap the stop", err)
			}
			checkSavedLate(t, r, err)
			if _, err := r.store.Load(testOwner, "tester"); err != nil {
				t.Errorf("the mate is not saved: %v", err)
			}
			if got := <-answered; got.status != http.StatusFound {
				t.Errorf("/created answered %+v, want the redirect to the install page", got)
			}
			if _, lookups, _ := r.api.counts(); lookups != 0 {
				t.Errorf("GitHub got %d lookups, want none after the stop", lookups)
			}
		})
	}
}

func TestAStopThatCancelsTheConversionSaysToDeleteTheApp(t *testing.T) {
	for name, stop := range stopsMidExchange() {
		t.Run(name, func(t *testing.T) {
			r, late, answered := lateRun(t, true)
			stop(r, late)
			err := r.create(t, "tester")
			if err == nil || !strings.Contains(err.Error(), "GitHub may have created the app crew-tester") {
				t.Fatalf("Create = %v, want the conversion's failure", err)
			}
			if _, err := r.store.Load(testOwner, "tester"); !errors.Is(err, ErrNoBot) {
				t.Errorf("Load = %v, want nothing saved", err)
			}
			if got := <-answered; got.status != http.StatusInternalServerError {
				t.Errorf("/created answered %+v, want 500", got)
			}
		})
	}
}

func TestTheInstallWaitRidesOutTransientErrors(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.statuses = []int{http.StatusNotFound, http.StatusBadGateway, http.StatusTooManyRequests,
		http.StatusNotFound, http.StatusOK}
	if err := r.create(t, "tester"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, lookups, tokens := r.api.counts(); lookups != 5 || tokens != 1 {
		t.Errorf("GitHub got %d lookups and %d token calls, want 5 and 1", lookups, tokens)
	}
}

func TestTheInstallWaitStopsAtOnceOnAClientError(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.statuses = []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusOK}
	err := r.create(t, "tester")
	if err == nil || !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), testInstallURL) {
		t.Fatalf("Create = %v, want the 422 and the install URL", err)
	}
	if _, lookups, _ := r.api.counts(); lookups != 2 {
		t.Errorf("GitHub got %d lookups, want 2", lookups)
	}
}

func TestTheInstallTimeoutNamesTheLastTransientError(t *testing.T) {
	r := newRun(t, testOwner, true)
	r.api.statuses, r.api.lookupStatus = []int{http.StatusNotFound}, http.StatusBadGateway
	r.flow.InstallTimeout = 30 * time.Millisecond
	err := r.create(t, "tester")
	if err == nil || !strings.Contains(err.Error(), "is not installed on thatsnotmynameio/crew after") ||
		!strings.Contains(err.Error(), "502 Bad Gateway") {
		t.Fatalf("Create = %v, want the timeout and the last 502", err)
	}
	if _, lookups, _ := r.api.counts(); lookups < 2 {
		t.Errorf("GitHub got %d lookups, want the wait to go on", lookups)
	}
}
