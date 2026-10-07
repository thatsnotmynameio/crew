package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// gh calls the gh CLI through a proc.Runner. It works on the repository gh
// resolves from crew's working directory, the repository's root. It reads as
// you, gh's own login, and writes as the writer, one of crew's bots,
// falling back to you when the bot cannot write (KTD4).
type gh struct {
	run proc.Runner

	mu     sync.Mutex
	login  string        // the authenticated user, once resolved
	writer port.Identity // who writes; the zero Identity is you
	// lost is the warning crew wrote when writes went back to you for
	// the rest of the run; "" while they go as the writer.
	lost string
}

// call runs gh with args as you and returns what it printed. On a
// non-zero exit the error carries gh's stderr, and Output still holds it for
// classifying.
func (g *gh) call(ctx context.Context, args ...string) (proc.Output, error) {
	return g.run(ctx, proc.Command{Name: "gh", Args: args})
}

// decode runs gh with args as you and decodes its JSON output into v.
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

// known returns the login gh is authenticated as once viewer resolved it,
// "" before.
func (g *gh) known() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.login
}

// actAs makes writer write from now on.
func (g *gh) actAs(writer port.Identity) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.writer = writer
}

// bot returns the bot that writes, and whether one does: not when the
// writer is you, or writes went back to you.
func (g *gh) bot() (port.Identity, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.writer, g.writer.Login != "" && g.lost == ""
}

// backToLogin makes every later write of the run go as you, because
// GitHub refused bot's write for kind. The first call records the warning
// lostWarning words; a later one, from a write that raced it, keeps it.
func (g *gh) backToLogin(kind refusalKind, bot crew.BotName) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lost == "" {
		g.lost = lostWarning(kind, bot)
	}
}

// writerLost returns the warning backToLogin recorded, "" before.
func (g *gh) writerLost() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lost
}

// lostWarning returns the warning crew writes when bot's writes went back
// to you because GitHub refused one for kind: what happened, what fixes
// it, and that crew writes as you until it restarts. It quotes nothing
// gh printed.
func lostWarning(kind refusalKind, bot crew.BotName) string {
	create := fmt.Sprintf("run `crew bots create %s` in this repository", bot)
	var why string
	switch kind {
	case refusedCredentials:
		why = "GitHub refused its credentials, as when its token was revoked or expired; restart crew, and " +
			create + " if it happens again"
	case refusedPermission:
		why = "GitHub refused it a permission; " + create
	default:
		why = "it lost access to the repository; " + create + " to install it"
	}
	return fmt.Sprintf("crew's writes as bot %s went back to you: %s; crew writes as you until it restarts", bot, why)
}

// write runs gh with args as the writer and returns what it printed and the
// login of the bot it wrote as, "" when it wrote as you. A bot whose
// write fails is handled as KTD4 says:
//
//   - refused for its credentials, it renews its token and tries once more;
//   - still refused, or refused a permission, the write runs again as
//     you, and so does every later write of the run;
//   - answered 404 or 410, the write runs again as you, and when that
//     write gets through, every later write of the run goes as you too.
//
// Any other failure is the bot's, as is.
func (g *gh) write(ctx context.Context, args ...string) (proc.Output, string, error) {
	w, ok := g.bot()
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
	switch kind := refusal(out); kind {
	case refusedCredentials, refusedPermission:
		g.backToLogin(kind, w.Bot)
		out, err = g.call(ctx, args...)
		return out, "", err
	case refusedNotFound:
		out, err = g.call(ctx, args...)
		if err == nil {
			g.backToLogin(kind, w.Bot)
		}
		return out, "", err
	default:
		return out, w.Login, err
	}
}

// refusalKind is why GitHub refused a bot's write, as gh printed it.
type refusalKind int

// The refusals a bot's write can meet that KTD4 handles.
const (
	refusedOther       refusalKind = iota // anything else, such as a locked issue or a rate limit
	refusedCredentials                    // HTTP 401 or bad credentials: the token expired or was revoked
	refusedPermission                     // the bot lacks a permission, or gh holds no login
	refusedNotFound                       // HTTP 404 or 410: the bot lost access, or the item is gone
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
