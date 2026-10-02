package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// runner runs a command to completion, as proc.Group.Run does: a non-zero
// exit is an error carrying the command's stderr, and Output holds what it
// printed either way. Tests inject a scripted one.
type runner func(ctx context.Context, c proc.Command) (proc.Output, error)

// gh calls the gh CLI through a runner. It works on the repository gh
// resolves from crew's working directory, the repository's root.
type gh struct {
	run runner

	mu    sync.Mutex
	login string // the authenticated user, once resolved
}

// call runs gh with args and returns what it printed. On a non-zero exit the
// error carries gh's stderr, and Output still holds it for classifying.
func (g *gh) call(ctx context.Context, args ...string) (proc.Output, error) {
	return g.run(ctx, proc.Command{Name: "gh", Args: args})
}

// decode runs gh with args and decodes its JSON output into v.
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
