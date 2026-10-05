package bots

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// The renewal loop's pace (KTD2): it looks at the wall clock every minute
// and renews a token with fewer than renewBefore left, so a machine waking
// from sleep renews at once.
const (
	renewEvery  = time.Minute
	renewBefore = 10 * time.Minute
)

// The files of a gh config directory, and gh's own config in it.
const (
	ghConfigFile = "config.yml"
	ghHostsFile  = "hosts.yml"
	ghConfig     = "version: \"1\"\n"
	filePerm     = 0o600
)

// botUnset returns the inherited variables a child acting as a bot must
// not get: each would win over its GH_CONFIG_DIR.
func botUnset() []string {
	return []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_HOST"}
}

// mintFunc mints a token of b's installation id for the repository called
// repo, as Client.AccessToken does.
type mintFunc func(ctx context.Context, b Bot, id int64, repo string) (Grant, error)

// ActOptions are what Act needs to make the configured bots act.
type ActOptions struct {
	// Run runs gh and git as you, in Root.
	Run proc.Runner
	// Store keeps the bots.
	Store *Store
	// Client calls GitHub's REST API.
	Client *Client
	// Root is the repository's root.
	Root string
	// Names are the configured bots, the default first.
	Names []string
	// Default is the default bot's name, or "" for none.
	Default string
	// TempDir is the parent of the run's private directory; "" chooses
	// $XDG_RUNTIME_DIR when set, else the system's temporary directory.
	TempDir string
	// Getenv reads crew's environment, such as os.Getenv.
	Getenv func(string) string
	// Step, when not nil, is called with each step of Act just before it
	// starts, in plain words such as "making bot ops act", so a slow start
	// shows what it waits on. nil reports nothing.
	Step func(step string)

	// mint, when not nil, replaces Client.AccessToken, so tests drive the
	// renewal loop without the network.
	mint mintFunc
}

// ActingBot is a bot that acts this run.
type ActingBot struct {
	// Name is the bot's name.
	Name string
	// Login is its bot's login, such as crew-ops[bot].
	Login string
	// Env holds the KEY=value entries that make a session or a check act as
	// the bot: GH_CONFIG_DIR of its sessions' directory, and the git config
	// of the co-author hook and of your pinned credential helper when they
	// apply.
	Env []string
	// Unset are the inherited variables a child acting as the bot must not
	// get.
	Unset []string
	// WriterEnv, set for the default bot alone, holds GH_CONFIG_DIR of a
	// directory only crew's own writes use, so no session can log it out.
	WriterEnv []string
}

// Acting is the bots acting this run: their tokens, in a private directory
// outside the repository, and the loop that renews them.
type Acting struct {
	// Bots are the bots that act, in the order they were configured.
	Bots []ActingBot
	// Logins are the bot logins of the configured bots stored on this
	// machine, in the order they were configured, whether or not they act:
	// crew still takes the issues a bot that cannot act this run opened.
	Logins []string
	// Warnings say, one line each, which configured bot cannot act or
	// adds no co-author, why, and the fix.
	Warnings []string
	// Unable holds, for each configured bot that cannot act this run, the
	// short reason why: "no key", "bad key file", "not installed", "key
	// rejected" or "no token". A bot that acts without a co-author has
	// none.
	Unable map[string]string

	dir      string
	repo     string
	mint     mintFunc
	mu       sync.Mutex // held while a token is renewed and its files written
	acted    []*acted
	renewals renewals
	cancel   context.CancelFunc
	done     chan struct{}
	closed   sync.Once
}

// The short reasons of Acting.Unable.
const (
	reasonNoKey        = "no key"
	reasonBadKeyFile   = "bad key file"
	reasonNotInstalled = "not installed"
	reasonKeyRejected  = "key rejected"
	reasonNoToken      = "no token"
)

// cannot is why a configured bot cannot act: its warning, and the short
// reason of Acting.Unable.
type cannot struct {
	warning, reason string
}

// acted is a bot that acts, and where its token is.
type acted struct {
	bot Bot
	// path is the bot's file, the one Store.Load read.
	path string
	// inst is its installation on the repository.
	inst int64
	// sessionsDir is the gh config directory of its sessions and checks.
	sessionsDir string
	// writerDir, for the default bot alone, is the gh config directory of
	// crew's own writes; "" for any other bot.
	writerDir string
	// expires is when its token stops working.
	expires time.Time
}

// Act makes the configured bots act on the repository at Root: for each
// one it loads the bot, finds its installation, mints a token limited to
// the repository and writes gh config directories holding it, under a new
// private directory outside the repository. A loop then renews every token
// before it expires, until Close. ctx bounds only the startup work.
//
// An invalid name, a repository gh cannot resolve, git failing and a
// private directory that cannot be made are errors, after which nothing
// Act made remains; a bot that cannot act, or adds no co-author, is a
// warning.
func Act(ctx context.Context, o ActOptions) (*Acting, error) {
	for _, name := range o.Names {
		if err := CheckName(name); err != nil {
			return nil, err
		}
	}
	if len(o.Names) == 0 {
		return &Acting{}, nil
	}
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	o.step("resolving the repository for the bots")
	repo, err := ResolveRepo(ctx, o.Run, o.Root)
	if err != nil {
		return nil, err
	}
	o.step("checking git for the bots")
	git, err := probeGit(ctx, o.Run, o.Root)
	if err != nil {
		return nil, err
	}
	loginDir, err := loginGhDir(o.Getenv)
	if err != nil {
		return nil, err
	}
	o.step("making a private directory for the bots' tokens")
	dir, err := runDir(o)
	if err != nil {
		return nil, err
	}
	a := &Acting{dir: dir, repo: repo.Name, mint: o.mint}
	if a.mint == nil {
		a.mint = o.Client.AccessToken
	}
	r := resolver{o: o, repo: repo, git: git, loginDir: loginDir, a: a}
	for _, name := range o.Names {
		o.step(fmt.Sprintf("making bot %s act", name))
		if err := r.resolve(ctx, name); err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
	}
	a.start(ctx)
	return a, nil
}

// step reports step through o.Step, when it is set.
func (o ActOptions) step(step string) {
	if o.Step != nil {
		o.Step(step)
	}
}

// runDir makes the run's private directory, mode 0700, and refuses one
// inside the repository.
func runDir(o ActOptions) (string, error) {
	parent := o.TempDir
	if parent == "" {
		parent = o.Getenv("XDG_RUNTIME_DIR")
	}
	if parent == "" {
		parent = os.TempDir()
	}
	if within(o.Root, parent) {
		return "", envErrorf("crew would keep the bots' tokens in %s, inside the repository %s; "+
			"set XDG_RUNTIME_DIR or TMPDIR to a directory outside it", parent, o.Root)
	}
	dir, err := os.MkdirTemp(parent, "crew-bots-")
	if err != nil {
		return "", envErrorf("make a directory for the bots' tokens: %w", err)
	}
	return dir, nil
}

// within reports whether path is root or inside it, symbolic links
// resolved.
func within(root, path string) bool {
	rel, err := filepath.Rel(resolved(root), resolved(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolved returns path made absolute, its symbolic links resolved when it
// exists.
func resolved(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if target, err := filepath.EvalSymlinks(path); err == nil {
		return target
	}
	return path
}

// resolver turns each configured name into an acting bot or a warning.
type resolver struct {
	o        ActOptions
	repo     Repo
	git      gitSetup
	loginDir string
	a        *Acting
}

// resolve makes the bot called name act, or warns why it cannot. Its error
// is a file it could not write.
func (r *resolver) resolve(ctx context.Context, name string) error {
	s, why := r.find(ctx, name)
	if why.warning != "" {
		r.unable(name, why)
		return nil
	}
	s.sessionsDir = filepath.Join(r.a.dir, name, "sessions")
	if name == r.o.Default {
		s.writerDir = filepath.Join(r.a.dir, name, "crew")
	}
	g, err := r.a.mintChecked(ctx, s)
	if err != nil {
		r.unable(name, tokenCannot(s.bot, s.path, err))
		return nil
	}
	for _, dir := range s.dirs() {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("make the gh directory of bot %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, ghConfigFile), []byte(ghConfig), filePerm); err != nil {
			return fmt.Errorf("write the gh config of bot %s: %w", name, err)
		}
	}
	if err := s.write(g); err != nil {
		return err
	}
	r.a.acted = append(r.a.acted, s)
	r.a.Bots = append(r.a.Bots, r.bot(ctx, s, g.Token))
	return nil
}

// unable records that the bot called name cannot act, and why.
func (r *resolver) unable(name string, why cannot) {
	r.a.Warnings = append(r.a.Warnings, why.warning)
	if r.a.Unable == nil {
		r.a.Unable = map[string]string{}
	}
	r.a.Unable[name] = why.reason
}

// find loads the bot called name and finds its installation on the
// repository, or returns why it cannot act; its warning is "" when it can.
// The bot it returns has no directory yet.
func (r *resolver) find(ctx context.Context, name string) (*acted, cannot) {
	b, path, err := r.o.Store.Load(r.repo.Owner, name)
	switch {
	case errors.Is(err, ErrNoBot):
		return nil, cannot{fmt.Sprintf("bot %s has no key on this machine for %s; "+
			"run `crew bots create %s` in this repository", name, r.repo.Owner, name), reasonNoKey}
	case err != nil:
		return nil, cannot{fmt.Sprintf("bot %s cannot act: %v; "+
			"delete its file and run `crew bots create %s` in this repository", name, err, name), reasonBadKeyFile}
	case !validSlug(b.Slug):
		return nil, cannot{fmt.Sprintf("bot %s cannot act: its file %s holds an invalid app slug; "+
			"delete it and run `crew bots create %s` in this repository", name, path, name),
			reasonBadKeyFile}
	}
	r.a.Logins = append(r.a.Logins, botLogin(b.Slug))
	inst, err := r.o.Client.RepoInstallation(ctx, b, r.repo.Owner, r.repo.Name)
	if errors.Is(err, ErrNotInstalled) {
		return nil, cannot{fmt.Sprintf("bot %s is not installed on %s/%s; "+
			"run `crew bots create %s` in this repository", name, r.repo.Owner, r.repo.Name, name), reasonNotInstalled}
	}
	if err != nil {
		return nil, tokenCannot(b, path, err)
	}
	return &acted{bot: b, path: path, inst: inst.ID}, cannot{}
}

// tokenCannot is why bot b, whose file is at path, cannot act, as it could
// not get a token because of err.
func tokenCannot(b Bot, path string, err error) cannot {
	if errors.Is(err, ErrKeyRejected) {
		return cannot{tokenWarning(b, path, err), reasonKeyRejected}
	}
	return cannot{tokenWarning(b, path, err), reasonNoToken}
}

// tokenWarning is the warning of bot b, whose file is at path, which could
// not get a token because of err.
func tokenWarning(b Bot, path string, err error) string {
	if errors.Is(err, ErrKeyRejected) {
		return fmt.Sprintf("GitHub rejected the key of bot %s; delete %s and "+
			"run `crew bots create %s` in this repository", b.Name, path, b.Name)
	}
	return fmt.Sprintf("bot %s could not get a token: %v; crew acts as you in its place this run", b.Name, err)
}

// bot returns s's acting bot, whose token is token. It warns when the
// bot's commits will carry no co-author.
func (r *resolver) bot(ctx context.Context, s *acted, token Token) ActingBot {
	login := botLogin(s.bot.Slug)
	var entries []configEntry
	if r.git.hooks {
		id, err := r.o.Client.BotUserID(ctx, token, s.bot.Slug)
		if err == nil {
			entries = hookEntries(coAuthorTrailer(s.bot.Slug, id))
		} else {
			r.a.Warnings = append(r.a.Warnings, fmt.Sprintf("bot %s's commits carry no co-author: "+
				"crew could not find the user id of %s (%v); restart crew to try again", s.bot.Name, login, err))
		}
	} else {
		r.a.Warnings = append(r.a.Warnings, fmt.Sprintf("bot %s's commits carry no co-author: "+
			"%s is older than git 2.54, which runs the co-author hook; update git", s.bot.Name, r.git.version))
	}
	if r.git.helper != "" {
		entries = append(entries, credentialEntries(r.git.helper, r.loginDir)...)
	}
	am := ActingBot{
		Name:  s.bot.Name,
		Login: login,
		Env: append([]string{"GH_CONFIG_DIR=" + s.sessionsDir},
			configEnv(r.o.Getenv("GIT_CONFIG_COUNT"), entries)...),
		Unset: botUnset(),
	}
	if s.writerDir != "" {
		am.WriterEnv = []string{"GH_CONFIG_DIR=" + s.writerDir}
	}
	return am
}

// Renew renews the token of the bot called name at once and rewrites its
// files. It fails when the bot does not act this run, or no token came.
func (a *Acting) Renew(ctx context.Context, name string) error {
	i := slices.IndexFunc(a.acted, func(s *acted) bool { return s.bot.Name == name })
	if i < 0 {
		return fmt.Errorf("bot %s does not act this run", name)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.renew(ctx, a.acted[i])
}

// Close stops the renewal loop, waits for it to end and removes the run's
// directory with every token in it. Calling it again does nothing.
func (a *Acting) Close() {
	a.closed.Do(func() {
		if a.cancel != nil {
			a.cancel()
			<-a.done
		}
		if a.dir != "" {
			_ = os.RemoveAll(a.dir)
		}
	})
}

// start starts the renewal loop, on a context of its own that only Close
// ends.
func (a *Acting) start(ctx context.Context) {
	loop, cancel := context.WithCancel(context.WithoutCancel(ctx))
	a.cancel, a.done = cancel, make(chan struct{})
	go a.loop(loop)
}

// loop renews, every renewEvery, each token with fewer than renewBefore
// left, until ctx ends. A failed renewal is tried again at the next check.
func (a *Acting) loop(ctx context.Context) {
	defer close(a.done)
	tick := time.NewTicker(renewEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		for _, s := range a.acted {
			a.mu.Lock()
			if time.Until(s.expires) < renewBefore {
				_ = a.renew(ctx, s)
			}
			a.mu.Unlock()
		}
	}
}

// renew mints a new token of s and writes it, and records how it went for
// Failing. a.mu must be held.
func (a *Acting) renew(ctx context.Context, s *acted) error {
	g, err := a.mintChecked(ctx, s)
	if err == nil {
		err = s.write(g)
	}
	a.renewals.record(s.bot.Name, renewWarning(s.bot, s.path, err))
	if err != nil {
		return fmt.Errorf("renew the token of bot %s: %w", s.bot.Name, err)
	}
	return nil
}

// mintChecked mints a token of s and checks that it grants exactly what
// crew asked for.
func (a *Acting) mintChecked(ctx context.Context, s *acted) (Grant, error) {
	g, err := a.mint(ctx, s.bot, s.inst, a.repo)
	if err != nil {
		return Grant{}, err
	}
	if !usableToken(g.Token) {
		return Grant{}, errors.New("GitHub's reply holds no usable token")
	}
	if asked := permissions(); !maps.Equal(g.Permissions, asked) {
		return Grant{}, fmt.Errorf("GitHub granted %s, not the %s crew asked for",
			formatPermissions(g.Permissions), formatPermissions(asked))
	}
	return g, nil
}

// usableToken reports whether token can go into hosts.yml as it is: ghs_
// and one or more letters, digits, underscores, hyphens and dots, as
// GitHub's installation tokens are, the stateless ghs_APPID_JWT ones
// included.
func usableToken(token Token) bool {
	rest, ok := strings.CutPrefix(string(token), "ghs_")
	return ok && rest != "" && !strings.ContainsFunc(rest, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' && r != '.'
	})
}

// formatPermissions returns perms as name:level pairs, sorted.
func formatPermissions(perms map[string]string) string {
	pairs := make([]string, 0, len(perms))
	for _, name := range slices.Sorted(maps.Keys(perms)) {
		pairs = append(pairs, name+":"+perms[name])
	}
	return strings.Join(pairs, " ")
}

// dirs returns s's gh config directories: its sessions', then crew's own
// when it has one.
func (s *acted) dirs() []string {
	if s.writerDir == "" {
		return []string{s.sessionsDir}
	}
	return []string{s.sessionsDir, s.writerDir}
}

// write writes g's token into each of s's directories and records when it
// expires. Each hosts.yml is replaced by a rename, so a gh reading it never
// sees half a file.
func (s *acted) write(g Grant) error {
	login := botLogin(s.bot.Slug)
	token := string(g.Token)
	data := []byte("github.com:\n    users:\n        " + login + ":\n            oauth_token: " + token +
		"\n    git_protocol: https\n    oauth_token: " + token + "\n    user: " + login + "\n")
	for _, dir := range s.dirs() {
		tmp, err := writeTemp(dir, "hosts", data)
		if err != nil {
			return fmt.Errorf("write the token of bot %s: %w", s.bot.Name, err)
		}
		if err := os.Rename(tmp, filepath.Join(dir, ghHostsFile)); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("write the token of bot %s: %w", s.bot.Name, err)
		}
	}
	s.expires = g.ExpiresAt
	return nil
}
