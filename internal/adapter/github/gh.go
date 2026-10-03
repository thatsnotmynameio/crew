package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// gh calls the gh CLI through a proc.Runner. It works on the repository gh
// resolves from crew's working directory, the repository's root. It reads as
// the boss, gh's own login, and writes as the writer, one of crew's mates,
// falling back to the boss when the mate cannot write (KTD4).
type gh struct {
	run proc.Runner

	mu     sync.Mutex
	login  string        // the authenticated user, once resolved
	writer port.Identity // who writes; the zero Identity is the boss
	asBoss bool          // whether writes went back to the boss for the rest of the run
}

// call runs gh with args as the boss and returns what it printed. On a
// non-zero exit the error carries gh's stderr, and Output still holds it for
// classifying.
func (g *gh) call(ctx context.Context, args ...string) (proc.Output, error) {
	return g.run(ctx, proc.Command{Name: "gh", Args: args})
}

// decode runs gh with args as the boss and decodes its JSON output into v.
func (g *gh) decode(ctx context.Context, v any, args ...string) error {
	out, err := g.call(ctx, args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out.Stdout, v); err != nil {
		return fmt.Errorf("gh %s: unreadable output: %w", strings.Join(args[:2], " "), err)
	}
	return nil
}

// viewer returns the login gh is authenticated as. It asks gh once and
// remembers the answer; a failed lookup is asked again next time.
func (g *gh) viewer(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.login != "" {
		return g.login, nil
	}
	out, err := g.call(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", fmt.Errorf("resolve the gh login: %w", err)
	}
	login := strings.TrimSpace(string(out.Stdout))
	if login == "" {
		return "", errors.New("resolve the gh login: gh api user printed no login")
	}
	g.login = login
	return login, nil
}

// actAs makes writer write from now on.
func (g *gh) actAs(writer port.Identity) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.writer = writer
}

// mate returns the mate that writes, and whether one does: not when the
// writer is the boss, or writes went back to the boss.
func (g *gh) mate() (port.Identity, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.writer, g.writer.Login != "" && !g.asBoss
}

// backToBoss makes every later write of the run go as the boss.
func (g *gh) backToBoss() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asBoss = true
}

// write runs gh with args as the writer and returns what it printed and the
// login of the mate it wrote as, "" when it wrote as the boss. A mate whose
// write fails is handled as KTD4 says:
//
//   - refused for its credentials, it renews its token and tries once more;
//   - still refused, or refused a permission, the write runs again as the
//     boss, and so does every later write of the run;
//   - answered 404 or 410, the write runs again as the boss, and when the
//     boss gets through, every later write of the run goes as the boss too.
//
// Any other failure is the mate's, as is.
func (g *gh) write(ctx context.Context, args ...string) (proc.Output, string, error) {
	w, ok := g.mate()
	if !ok {
		out, err := g.call(ctx, args...)
		return out, "", err
	}
	run := func() (proc.Output, error) {
		return g.run(ctx, proc.Command{Name: "gh", Args: args, Env: w.Env, Unset: w.Unset})
	}
	out, err := run()
	if err != nil && refusal(out) == refusedCredentials && w.Renew != nil && w.Renew(ctx) == nil {
		out, err = run()
	}
	if err == nil {
		return out, w.Login, nil
	}
	switch refusal(out) {
	case refusedCredentials, refusedPermission:
		g.backToBoss()
		out, err = g.call(ctx, args...)
		return out, "", err
	case refusedNotFound:
		out, err = g.call(ctx, args...)
		if err == nil {
			g.backToBoss()
		}
		return out, "", err
	default:
		return out, w.Login, err
	}
}

// refusalKind is why GitHub refused a mate's write, as gh printed it.
type refusalKind int

// The refusals a mate's write can meet that KTD4 handles.
const (
	refusedOther       refusalKind = iota // anything else, such as a locked issue or a rate limit
	refusedCredentials                    // HTTP 401 or bad credentials: the token expired or was revoked
	refusedPermission                     // the mate lacks a permission, or gh holds no login
	refusedNotFound                       // HTTP 404 or 410: the mate lost access, or the item is gone
)

// refusal returns why gh's failed call, which printed out, was refused.
func refusal(out proc.Output) refusalKind {
	stderr := string(out.Stderr)
	lower := strings.ToLower(stderr)
	switch code := httpStatus(stderr); {
	case code == http.StatusUnauthorized || strings.Contains(lower, "bad credentials"):
		return refusedCredentials
	case strings.Contains(lower, "resource not accessible by integration") ||
		strings.Contains(lower, "gh auth login"):
		return refusedPermission
	case code == http.StatusNotFound || code == http.StatusGone:
		return refusedNotFound
	default:
		return refusedOther
	}
}
