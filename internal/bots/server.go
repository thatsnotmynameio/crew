package bots

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
	// Name is the bot's name, App the app name suggested and Owner the
	// account that will own it.
	Name, App, Owner string
	// Action is GitHub's create page, with the run's state.
	Action string
	// Manifest is the manifest as JSON.
	Manifest string
}

// outcome is how GitHub's redirect ended: the saved bot, or why not.
type outcome struct {
	bot Bot
	err error
}

// server is the loopback server GitHub's manifest flow comes back to
// (KTD3). GET / serves the form, and GET /created exchanges the code of
// GitHub's redirect once its state is the run's; any other path is 404,
// and a request for any host but its own is 421.
type server struct {
	form  form
	state string
	// exchange converts a code and saves the bot; a bot with an error
	// was saved all the same.
	exchange func(ctx context.Context, code string) (Bot, error)
	// installURL returns the page that installs a bot.
	installURL func(m Bot) string
	// done receives the outcome of the one exchange.
	done chan outcome

	mu      sync.Mutex
	handled bool // an exchange ran
	saved   bool // and saved the bot without an error
}

// serve serves on ln until the returned stop is called. The exchange runs
// with a context that stop cancels, and stop returns once the server has
// shut down, after the request being answered, if any. Calling stop again
// does nothing.
func (s *server) serve(ctx context.Context, ln net.Listener) func() {
	ctx, cancel := context.WithCancel(ctx)
	page := template.Must(template.New("form").Parse(formPage))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) { s.page(w, page) })
	mux.HandleFunc("GET /created", func(w http.ResponseWriter, r *http.Request) { s.created(ctx, w, r) })
	srv := &http.Server{Handler: onlyHosts(ln.Addr(), mux), ReadHeaderTimeout: readHeaderTimeout}
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(ln)
	}()
	return sync.OnceFunc(func() {
		cancel()
		sctx, scancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer scancel()
		if srv.Shutdown(sctx) != nil {
			_ = srv.Close()
		}
		<-served
	})
}

// onlyHosts passes to next only the requests for addr, the listener's
// 127.0.0.1:<port>, or for localhost:<port>, which a browser over an SSH
// forward may use. Any other host gets 421, so a page of another site that
// rebinds its name to 127.0.0.1 cannot read the form and its state.
func onlyHosts(addr net.Addr, next http.Handler) http.Handler {
	own := addr.String()
	_, port, _ := net.SplitHostPort(own) // a TCP address always has a port
	local := net.JoinHostPort("localhost", port)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != own && r.Host != local {
			http.Error(w, "crew answers only at http://"+own+"/.", http.StatusMisdirectedRequest)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// page answers with the form, filled in by page, the parsed formPage.
func (s *server) page(w http.ResponseWriter, page *template.Template) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = page.Execute(w, s.form)
}

// created answers GitHub's redirect. A wrong or missing state gets 400, and
// the wait goes on. The first redirect with the right state exchanges its
// code, delivers the outcome and, once the bot is saved, sends the browser
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
	s.done <- outcome{bot: m, err: err}
	if err != nil {
		http.Error(w, "crew could not finish creating the mate; see crew's output.", http.StatusInternalServerError)
		return
	}
	s.saved = true
	// The target is GitHub's install page for the app GitHub just created:
	// its host is fixed, and only the escaped slug comes from GitHub's reply.
	// nosemgrep: go.lang.security.injection.open-redirect.open-redirect
	http.Redirect(w, r, s.installURL(m), http.StatusFound)
}
