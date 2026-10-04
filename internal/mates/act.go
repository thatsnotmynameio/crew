package mates

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

// mateUnset returns the inherited variables a child acting as a mate must
// not get: each would win over its GH_CONFIG_DIR.
func mateUnset() []string {
	return []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_HOST"}
}

// mintFunc mints a token of m's installation id for the repository called
// repo, as Client.AccessToken does.
type mintFunc func(ctx context.Context, m Mate, id int64, repo string) (Grant, error)

// ActOptions are what Act needs to make the configured mates act.
type ActOptions struct {
	// Run runs gh and git as the boss, in Root.
	Run proc.Runner
	// Store keeps the mates.
	Store *Store
	// Client calls GitHub's REST API.
	Client *Client
	// Root is the repository's root.
	Root string
	// Names are the configured mates, the default first.
	Names []string
	// Default is the default mate's name, or "" for none.
	Default string
	// TempDir is the parent of the run's private directory; "" chooses
	// $XDG_RUNTIME_DIR when set, else the system's temporary directory.
	TempDir string
	// Getenv reads crew's environment, such as os.Getenv.
	Getenv func(string) string
	// Step, when not nil, is called with each step of Act just before it
	// starts, in plain words such as "making mate ops act", so a slow start
	// shows what it waits on. nil reports nothing.
	Step func(step string)

	// mint, when not nil, replaces Client.AccessToken, so tests drive the
	// renewal loop without the network.
	mint mintFunc
}

// ActingMate is a mate that acts this run.
type ActingMate struct {
	// Name is the mate's name.
	Name string
	// Login is its bot's login, such as crew-ops[bot].
	Login string
	// Env holds the KEY=value entries that make a session or a check act as
	// the mate: GH_CONFIG_DIR of its sessions' directory, and the git config
	// of the co-author hook and of the boss's pinned credential helper when
	// they apply.
	Env []string
	// Unset are the inherited variables a child acting as the mate must not
	// get.
	Unset []string
	// WriterEnv, set for the default mate alone, holds GH_CONFIG_DIR of a
	// directory only crew's own writes use, so no session can log it out.
	WriterEnv []string
}

// Acting is the mates acting this run: their tokens, in a private directory
// outside the repository, and the loop that renews them.
type Acting struct {
	// Mates are the mates that act, in the order they were configured.
	Mates []ActingMate
	// Logins are the bot logins of the configured mates stored on this
	// machine, in the order they were configured, whether or not they act:
	// crew still takes the issues a mate that cannot act this run opened.
	Logins []string
	// Warnings say, one line each, which configured mate cannot act or
	// adds no co-author, why, and the fix.
	Warnings []string

	dir    string
	repo   string
	mint   mintFunc
	mu     sync.Mutex // held while a token is renewed and its files written
	acted  []*acted
	cancel context.CancelFunc
	done   chan struct{}
	closed sync.Once
}

// acted is a mate that acts, and where its token is.
type acted struct {
	mate Mate
	// inst is its installation on the repository.
	inst int64
	// sessionsDir is the gh config directory of its sessions and checks.
	sessionsDir string
	// writerDir, for the default mate alone, is the gh config directory of
	// crew's own writes; "" for any other mate.
	writerDir string
	// expires is when its token stops working.
	expires time.Time
}

// Act makes the configured mates act on the repository at Root: for each
// one it loads the mate, finds its installation, mints a token limited to
// the repository and writes gh config directories holding it, under a new
// private directory outside the repository. A loop then renews every token
// before it expires, until Close. ctx bounds only the startup work.
//
// An invalid name, a repository gh cannot resolve, git failing and a
// private directory that cannot be made are errors, after which nothing
// Act made remains; a mate that cannot act, or adds no co-author, is a
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
	o.step("resolving the repository for the mates")
	repo, err := ResolveRepo(ctx, o.Run, o.Root)
	if err != nil {
		return nil, err
	}
	o.step("checking git for the mates")
	git, err := probeGit(ctx, o.Run, o.Root)
	if err != nil {
		return nil, err
	}
	boss, err := bossGhDir(o.Getenv)
	if err != nil {
		return nil, err
	}
	dir, err := runDir(o)
	if err != nil {
		return nil, err
	}
	a := &Acting{dir: dir, repo: repo.Name, mint: o.mint}
	if a.mint == nil {
		a.mint = o.Client.AccessToken
	}
	r := resolver{o: o, repo: repo, git: git, boss: boss, a: a}
	for _, name := range o.Names {
		o.step(fmt.Sprintf("making mate %s act", name))
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
		return "", envErrorf("crew would keep the mates' tokens in %s, inside the repository %s; "+
			"set XDG_RUNTIME_DIR or TMPDIR to a directory outside it", parent, o.Root)
	}
	dir, err := os.MkdirTemp(parent, "crew-mates-")
	if err != nil {
		return "", envErrorf("make a directory for the mates' tokens: %w", err)
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

// resolver turns each configured name into an acting mate or a warning.
type resolver struct {
	o    ActOptions
	repo Repo
	git  gitSetup
	boss string
	a    *Acting
}

// resolve makes the mate called name act, or warns why it cannot. Its error
// is a file it could not write.
func (r *resolver) resolve(ctx context.Context, name string) error {
	m, inst, warning := r.find(ctx, name)
	if warning != "" {
		r.a.Warnings = append(r.a.Warnings, warning)
		return nil
	}
	s := &acted{mate: m, inst: inst, sessionsDir: filepath.Join(r.a.dir, name, "sessions")}
	if name == r.o.Default {
		s.writerDir = filepath.Join(r.a.dir, name, "crew")
	}
	g, err := r.a.mintChecked(ctx, s)
	if err != nil {
		r.a.Warnings = append(r.a.Warnings, tokenWarning(m, r.o.Store, err))
		return nil
	}
	for _, dir := range s.dirs() {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("make the gh directory of mate %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, ghConfigFile), []byte(ghConfig), filePerm); err != nil {
			return fmt.Errorf("write the gh config of mate %s: %w", name, err)
		}
	}
	if err := s.write(g); err != nil {
		return err
	}
	r.a.acted = append(r.a.acted, s)
	r.a.Mates = append(r.a.Mates, r.mate(ctx, s, g.Token))
	return nil
}

// find loads the mate called name and finds its installation on the
// repository, or returns the warning saying why it cannot.
func (r *resolver) find(ctx context.Context, name string) (Mate, int64, string) {
	m, err := r.o.Store.Load(r.repo.Owner, name)
	switch {
	case errors.Is(err, ErrNoMate):
		return Mate{}, 0, fmt.Sprintf("mate %s has no key on this machine for %s; "+
			"run `crew mates create %s` in this repository", name, r.repo.Owner, name)
	case err != nil:
		return Mate{}, 0, fmt.Sprintf("mate %s cannot act: %v; "+
			"delete its file and run `crew mates create %s` in this repository", name, err, name)
	case !validSlug(m.Slug):
		return Mate{}, 0, fmt.Sprintf("mate %s cannot act: its file %s holds an invalid app slug; "+
			"delete it and run `crew mates create %s` in this repository", name, r.o.Store.Path(m.Owner, name), name)
	}
	r.a.Logins = append(r.a.Logins, botLogin(m.Slug))
	inst, err := r.o.Client.RepoInstallation(ctx, m, r.repo.Owner, r.repo.Name)
	if errors.Is(err, ErrNotInstalled) {
		return Mate{}, 0, fmt.Sprintf("mate %s is not installed on %s/%s; "+
			"run `crew mates create %s` in this repository", name, r.repo.Owner, r.repo.Name, name)
	}
	if err != nil {
		return Mate{}, 0, tokenWarning(m, r.o.Store, err)
	}
	return m, inst.ID, ""
}

// tokenWarning is the warning of mate m, which could not get a token
// because of err.
func tokenWarning(m Mate, store *Store, err error) string {
	if errors.Is(err, ErrKeyRejected) {
		return fmt.Sprintf("GitHub rejected the key of mate %s; delete %s and "+
			"run `crew mates create %s` in this repository", m.Name, store.Path(m.Owner, m.Name), m.Name)
	}
	return fmt.Sprintf("mate %s could not get a token: %v; crew acts as the boss in its place this run", m.Name, err)
}

// mate returns s's acting mate, whose token is token. It warns when the
// mate's commits will carry no co-author.
func (r *resolver) mate(ctx context.Context, s *acted, token Token) ActingMate {
	login := botLogin(s.mate.Slug)
	var entries []configEntry
	if r.git.hooks {
		id, err := r.o.Client.BotUserID(ctx, token, s.mate.Slug)
		if err == nil {
			entries = hookEntries(coAuthorTrailer(s.mate.Slug, id))
		} else {
			r.a.Warnings = append(r.a.Warnings, fmt.Sprintf("mate %s's commits carry no co-author: "+
				"crew could not find the user id of %s (%v); restart crew to try again", s.mate.Name, login, err))
		}
	} else {
		r.a.Warnings = append(r.a.Warnings, fmt.Sprintf("mate %s's commits carry no co-author: "+
			"%s is older than git 2.54, which runs the co-author hook; update git", s.mate.Name, r.git.version))
	}
	if r.git.helper != "" {
		entries = append(entries, credentialEntries(r.git.helper, r.boss)...)
	}
	am := ActingMate{
		Name:  s.mate.Name,
		Login: login,
		Env: append([]string{"GH_CONFIG_DIR=" + s.sessionsDir},
			configEnv(r.o.Getenv("GIT_CONFIG_COUNT"), entries)...),
		Unset: mateUnset(),
	}
	if s.writerDir != "" {
		am.WriterEnv = []string{"GH_CONFIG_DIR=" + s.writerDir}
	}
	return am
}

// Renew renews the token of the mate called name at once and rewrites its
// files. It fails when the mate does not act this run, or no token came.
func (a *Acting) Renew(ctx context.Context, name string) error {
	i := slices.IndexFunc(a.acted, func(s *acted) bool { return s.mate.Name == name })
	if i < 0 {
		return fmt.Errorf("mate %s does not act this run", name)
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

// renew mints a new token of s and writes it. a.mu must be held.
func (a *Acting) renew(ctx context.Context, s *acted) error {
	g, err := a.mintChecked(ctx, s)
	if err != nil {
		return fmt.Errorf("renew the token of mate %s: %w", s.mate.Name, err)
	}
	return s.write(g)
}

// mintChecked mints a token of s and checks that it grants exactly what
// crew asked for.
func (a *Acting) mintChecked(ctx context.Context, s *acted) (Grant, error) {
	g, err := a.mint(ctx, s.mate, s.inst, a.repo)
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
	login := botLogin(s.mate.Slug)
	token := string(g.Token)
	data := []byte("github.com:\n    users:\n        " + login + ":\n            oauth_token: " + token +
		"\n    git_protocol: https\n    oauth_token: " + token + "\n    user: " + login + "\n")
	for _, dir := range s.dirs() {
		tmp, err := writeTemp(dir, "hosts", data)
		if err != nil {
			return fmt.Errorf("write the token of mate %s: %w", s.mate.Name, err)
		}
		if err := os.Rename(tmp, filepath.Join(dir, ghHostsFile)); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("write the token of mate %s: %w", s.mate.Name, err)
		}
	}
	s.expires = g.ExpiresAt
	return nil
}
