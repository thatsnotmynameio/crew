package bots

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// DefaultWeb is the base URL of GitHub's web pages.
const DefaultWeb = "https://github.com"

// The create flow's waits (KTD11): for GitHub's redirect once you
// created the app, for the installation, and between two lookups of it.
const (
	createTimeout  = 15 * time.Minute
	installTimeout = 15 * time.Minute
	pollInterval   = 3 * time.Second
)

// selectionAll is the repository_selection of an installation that covers
// every repository of its owner.
const selectionAll = "all"

// Flow is crew mates create: it creates a bot through GitHub's manifest
// flow, unless the repository's owner already has one of that name, then
// installs it on the repository and mints a token to prove it can act
// there. Its fields are its dependencies, so tests replace each one.
type Flow struct {
	// Gh runs gh, to resolve the repository.
	Gh proc.Runner
	// Open opens a URL in your browser, such as OpenBrowser. Its
	// failure only makes the flow say so.
	Open func(url string) error
	// Store keeps the bots.
	Store *Store
	// API calls GitHub's REST API.
	API *Client
	// Web is the base URL of GitHub's pages, such as DefaultWeb.
	Web string
	// CreateTimeout bounds the wait for GitHub's redirect, InstallTimeout
	// the wait for the installation; PollInterval spaces the lookups.
	CreateTimeout, InstallTimeout, PollInterval time.Duration
	// Stdout receives the progress lines, Stderr the warnings.
	Stdout, Stderr io.Writer
}

// NewFlow returns the flow that runs gh with gh, keeps bots in store and
// calls GitHub's API through api, with the browser, GitHub's pages and the
// waits crew uses for real.
func NewFlow(gh proc.Runner, store *Store, api *Client, stdout, stderr io.Writer) *Flow {
	return &Flow{
		Gh: gh, Open: OpenBrowser, Store: store, API: api, Web: DefaultWeb,
		CreateTimeout: createTimeout, InstallTimeout: installTimeout, PollInterval: pollInterval,
		Stdout: stdout, Stderr: stderr,
	}
}

// Create makes the bot called name ready on the GitHub repository of the
// git repository at root. It returns an *EnvError, for exit 2, when a check
// fails before anything was asked of GitHub's pages: an invalid name, no
// repository gh resolves, an unreadable saved bot or no loopback port.
// Any later failure is a plain error, for exit 1; so is ctx ending, which
// ends any wait at once.
func (f *Flow) Create(ctx context.Context, root, name string) error {
	if err := CheckName(name); err != nil {
		return err
	}
	repo, err := ResolveRepo(ctx, f.Gh, root)
	if err != nil {
		return err
	}
	m, err := f.Store.Load(repo.Owner, name)
	created := errors.Is(err, ErrNoBot)
	switch {
	case created:
		if m, err = f.create(ctx, repo, name); err != nil {
			return err
		}
	case err != nil:
		return err
	}
	// GitHub's redirect already took the browser to a new bot's install
	// page.
	return f.install(ctx, repo, m, !created)
}

// create creates the bot called name for repo's owner through the
// loopback server, and returns it once it is saved. It saves nothing
// unless GitHub's redirect comes back within CreateTimeout and before ctx
// ends.
func (f *Flow) create(ctx context.Context, repo Repo, name string) (Bot, error) {
	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return Bot{}, envErrorf("listen on a loopback port for GitHub's redirect: %w", err)
	}
	base := "http://" + ln.Addr().String()
	manifest, err := json.Marshal(NewManifest(name, repo.Owner, base+"/created"))
	if err != nil {
		_ = ln.Close()
		return Bot{}, fmt.Errorf("the app manifest: %w", err)
	}
	state := rand.Text()
	s := &server{
		form: form{
			Name: name, App: AppName(name), Owner: repo.Owner,
			Action: f.createURL(repo, state), Manifest: string(manifest),
		},
		state:      state,
		exchange:   func(ctx context.Context, code string) (Bot, error) { return f.keep(ctx, repo, name, code) },
		installURL: f.installURL,
		done:       make(chan outcome, 1),
	}
	stop := s.serve(ctx, ln)
	defer stop()

	f.sayf("opening %s/ to create the app %s for %s on GitHub", base, AppName(name), repo.Owner)
	f.open(base + "/")
	f.sayf("waiting for GitHub to create the app")
	return f.await(ctx, s, stop, name)
}

// await waits for the outcome of GitHub's redirect to s, for CreateTimeout
// or until ctx ends. When the wait ends first, it stops s, which waits for
// a redirect still being handled, and reports that redirect's outcome if
// one came: the bot may be saved all the same.
func (f *Flow) await(ctx context.Context, s *server, stop func(), name string) (Bot, error) {
	timer := time.NewTimer(f.CreateTimeout)
	defer timer.Stop()
	select {
	case out := <-s.done:
		if out.err != nil {
			return Bot{}, out.err
		}
		f.sayf("GitHub created the app %s; the mate %s is saved at %s",
			out.bot.AppName, name, f.Store.Path(out.bot.Owner, name))
		return out.bot, nil
	case <-timer.C:
		stop()
		if out, ok := lateOutcome(s); ok {
			return Bot{}, f.savedLate(name, out,
				fmt.Errorf("timed out after %v while crew handled GitHub's redirect", f.CreateTimeout))
		}
		return Bot{}, fmt.Errorf("GitHub did not create the app %s within %v; crew saved nothing", AppName(name),
			f.CreateTimeout)
	case <-ctx.Done():
		stop()
		if out, ok := lateOutcome(s); ok {
			return Bot{}, f.savedLate(name, out,
				fmt.Errorf("stopped while crew handled GitHub's redirect: %w", context.Cause(ctx)))
		}
		return Bot{}, fmt.Errorf("stopped before GitHub created the app %s: %w; crew saved nothing",
			AppName(name), context.Cause(ctx))
	}
}

// lateOutcome returns the outcome s delivered, if any, once s is stopped.
func lateOutcome(s *server) (outcome, bool) {
	select {
	case out := <-s.done:
		return out, true
	default:
		return outcome{}, false
	}
}

// savedLate returns the error of a wait that ended, as stopped says, while
// GitHub's redirect was being handled, and that redirect's outcome was out:
// out's own error, or stopped followed by where the bot is saved.
func (f *Flow) savedLate(name string, out outcome, stopped error) error {
	if out.err != nil {
		return out.err
	}
	return fmt.Errorf("%w; GitHub created the app %s and the mate %s is saved at %s: "+
		"crew mates create %s again installs it", stopped, out.bot.AppName, name, f.Store.Path(out.bot.Owner, name), name)
}

// keep exchanges code for the app GitHub created and saves it as the bot
// called name, under the account that owns the app. When that account is
// not repo's owner, the bot is saved all the same and keep returns it with
// an error naming both accounts.
func (f *Flow) keep(ctx context.Context, repo Repo, name, code string) (Bot, error) {
	conv, err := f.API.Convert(ctx, code)
	if err != nil {
		// GitHub creates the app before its redirect, so its key may be
		// lost with the conversion.
		return Bot{}, fmt.Errorf("%w; crew saved nothing, but GitHub may have created the app %s for %s already: "+
			"delete it at %s before running crew mates create %s again", err, AppName(name), repo.Owner,
			f.appsURL(repo), name)
	}
	m := conv.Bot(name)
	if err := f.Store.Save(m); err != nil {
		return Bot{}, fmt.Errorf("%w; GitHub created the app %s, but crew could not keep its key: "+
			"delete the app at %s before running crew mates create %s again", err, m.AppName, m.HTMLURL, name)
	}
	if !strings.EqualFold(m.Owner, repo.Owner) {
		return m, fmt.Errorf("GitHub created the app %s for %s, not for %s, which owns %s/%s; "+
			"crew kept the mate for %s at %s, and crew mates create %s in a repository of %s installs it there",
			m.AppName, m.Owner, repo.Owner, repo.Owner, repo.Name, m.Owner, f.Store.Path(m.Owner, name), name, m.Owner)
	}
	return m, nil
}

// install installs m on repo and confirms it: it mints a token for repo
// with m's key (R7). When m is not installed there yet, it prints the
// install page, opens it when open is set, and looks the installation up
// every PollInterval until it appears (KTD4). Every failure keeps the bot.
func (f *Flow) install(ctx context.Context, repo Repo, m Bot, open bool) error {
	where := repo.Owner + "/" + repo.Name
	installURL := f.installURL(m)
	inst, err := f.API.RepoInstallation(ctx, m, repo.Owner, repo.Name)
	if errors.Is(err, ErrNotInstalled) {
		f.sayf("install %s on %s at %s", m.AppName, where, installURL)
		if open {
			f.open(installURL)
		}
		f.sayf(`choose "Only select repositories" and select %s`, repo.Name)
		f.sayf("waiting for %s to be installed on %s", m.AppName, where)
		inst, err = f.poll(ctx, repo, m)
	}
	if err == nil {
		_, err = f.API.AccessToken(ctx, m, inst.ID, repo.Name)
	}
	switch {
	case errors.Is(err, ErrKeyRejected):
		return fmt.Errorf("%w; the app %s may have been deleted on GitHub: "+
			"delete %s and run crew mates create %s again to create a new mate",
			err, m.AppName, f.Store.Path(m.Owner, m.Name), m.Name)
	case err != nil:
		return fmt.Errorf("%w; crew kept the mate %s: install it at %s and run crew mates create %s again",
			err, m.Name, installURL, m.Name)
	}
	if inst.RepositorySelection == selectionAll {
		f.warnf("warning: %s is installed on every repository of %s, so the mate %s can act on all of them; "+
			"its installation settings on GitHub can limit it to selected repositories", m.AppName, repo.Owner, m.Name)
	}
	f.sayf("mate %s is ready on %s as %s", m.Name, where, m.BotLogin)
	return nil
}

// poll looks m's installation on repo up every PollInterval until it
// appears, InstallTimeout passes or ctx ends. Every lookup signs a fresh
// app JWT, as the wait outlasts one. A transient failure of a lookup, such
// as a 5xx or a rate limit, does not end the wait, but the timeout's error
// names the last one; any other failure ends it at once.
func (f *Flow) poll(ctx context.Context, repo Repo, m Bot) (Installation, error) {
	timeout := time.NewTimer(f.InstallTimeout)
	defer timeout.Stop()
	tick := time.NewTicker(f.PollInterval)
	defer tick.Stop()
	var failed error // the last transient failure
	for {
		select {
		case <-tick.C:
		case <-timeout.C:
			err := fmt.Errorf("%s is not installed on %s/%s after %v", m.AppName, repo.Owner, repo.Name, f.InstallTimeout)
			if failed != nil {
				err = fmt.Errorf("%w; the last lookup that failed: %w", err, failed)
			}
			return Installation{}, err
		case <-ctx.Done():
			return Installation{}, fmt.Errorf("stopped before %s was installed on %s/%s: %w",
				m.AppName, repo.Owner, repo.Name, context.Cause(ctx))
		}
		inst, err := f.API.RepoInstallation(ctx, m, repo.Owner, repo.Name)
		switch {
		case errors.Is(err, ErrNotInstalled):
		case err != nil && transient(err):
			failed = err
		default:
			return inst, err
		}
	}
}

// createURL returns GitHub's page that creates an app for repo's owner,
// carrying state.
func (f *Flow) createURL(repo Repo, state string) string {
	path := "/settings/apps/new"
	if repo.Org {
		path = "/organizations/" + url.PathEscape(repo.Owner) + path
	}
	return f.Web + path + "?" + url.Values{"state": {state}}.Encode()
}

// appsURL returns GitHub's page that lists the apps of repo's owner.
func (f *Flow) appsURL(repo Repo) string {
	if repo.Org {
		return f.Web + "/organizations/" + url.PathEscape(repo.Owner) + "/settings/apps"
	}
	return f.Web + "/settings/apps"
}

// installURL returns GitHub's page that installs m.
func (f *Flow) installURL(m Bot) string {
	return f.Web + "/apps/" + url.PathEscape(m.Slug) + "/installations/new"
}

// open opens u in the browser, or says it could not.
func (f *Flow) open(u string) {
	if err := f.Open(u); err != nil {
		f.warnf("could not open the browser (%v); open %s yourself", err, u)
	}
}

// sayf prints a progress line.
func (f *Flow) sayf(format string, args ...any) {
	_, _ = fmt.Fprintf(f.Stdout, "crew: "+format+"\n", args...)
}

// warnf prints a warning line.
func (f *Flow) warnf(format string, args ...any) {
	_, _ = fmt.Fprintf(f.Stderr, "crew: "+format+"\n", args...)
}
