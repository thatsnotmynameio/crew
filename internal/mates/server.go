package mates

import (
	"context"
	"crypto/subtle"
	"html/template"
	"net"
	"net/http"
	"sync"
	"time"
)

// The loopback server's limits: how long a request's headers may take, and
// how long its shutdown waits for a request still being answered.
const (
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 5 * time.Second
)

// formPage is the loopback page: a form that posts the manifest to GitHub's
// create page and submits itself, as GitHub accepts a manifest only as a
// form POST from the browser. html/template escapes every value.
const formPage = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>crew: create the mate {{.Name}}</title></head>
<body onload="document.forms[0].submit()">
<form method="post" action="{{.Action}}">
<input type="hidden" name="manifest" value="{{.Manifest}}">
<p>crew is taking you to GitHub to create the app {{.App}} for {{.Owner}}.</p>
<button type="submit">Continue to GitHub</button>
</form>
</body>
</html>
`

// form is what the loopback page holds.
type form struct {
	// Name is the mate's name, App the app name suggested and Owner the
	// account that will own it.
	Name, App, Owner string
	// Action is GitHub's create page, with the run's state.
	Action string
	// Manifest is the manifest as JSON.
	Manifest string
}

// outcome is how GitHub's redirect ended: the saved mate, or why not.
type outcome struct {
	mate Mate
	err  error
}

// server is the loopback server GitHub's manifest flow comes back to
// (KTD3). GET / serves the form, and GET /created exchanges the code of
// GitHub's redirect once its state is the run's; any other path is 404.
type server struct {
	form  form
	state string
	// exchange converts a code and saves the mate; a mate with an error
	// was saved all the same.
	exchange func(ctx context.Context, code string) (Mate, error)
	// installURL returns the page that installs a mate.
	installURL func(m Mate) string
	// done receives the outcome of the one exchange.
	done chan outcome

	mu      sync.Mutex
	handled bool // an exchange ran
	saved   bool // and saved the mate without an error
}

// serve serves on ln until the returned stop is called. The exchange runs
// with a context that stop cancels, and stop returns once the server has
// shut down.
func (s *server) serve(ctx context.Context, ln net.Listener) func() {
	ctx, cancel := context.WithCancel(ctx)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.page)
	mux.HandleFunc("GET /created", func(w http.ResponseWriter, r *http.Request) { s.created(ctx, w, r) })
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: readHeaderTimeout}
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(ln)
	}()
	return func() {
		cancel()
		sctx, scancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer scancel()
		if srv.Shutdown(sctx) != nil {
			_ = srv.Close()
		}
		<-served
	}
}

// page answers with the form.
func (s *server) page(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = template.Must(template.New("form").Parse(formPage)).Execute(w, s.form)
}

// created answers GitHub's redirect. A wrong or missing state gets 400, and
// the wait goes on. The first redirect with the right state exchanges its
// code, delivers the outcome and, once the mate is saved, sends the browser
// to the install page; any later one runs no exchange.
func (s *server) created(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(s.state)) != 1 {
		http.Error(w, "crew is not waiting for this redirect: its state is not this run's.", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.saved:
		http.Error(w, "the mate "+s.form.Name+" exists; crew created it already.", http.StatusOK)
		return
	case s.handled:
		http.Error(w, "crew already handled GitHub's redirect; see crew's output.", http.StatusConflict)
		return
	}
	s.handled = true
	m, err := s.exchange(ctx, q.Get("code"))
	s.done <- outcome{mate: m, err: err}
	if err != nil {
		http.Error(w, "crew could not finish creating the mate; see crew's output.", http.StatusInternalServerError)
		return
	}
	s.saved = true
	http.Redirect(w, r, s.installURL(m), http.StatusFound)
}
