package codex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// apiKey is the variable codex exec authenticates with instead of a stored
// login. codex login status ignores it.
const apiKey = "CODEX_API_KEY" //nolint:gosec // G101: the name of a variable crew reads, not a credential

// loginTimeout bounds codex login status, which reads a local file.
const loginTimeout = time.Minute

// loggedOut is what codex login status prints when nobody is logged in.
const loggedOut = "Not logged in"

// Prepare implements port.Preparer: it checks that codex is on PATH and
// logged in, and reports each step on ctx. A non-empty CODEX_API_KEY in
// crew's environment counts as a login, since codex exec uses it. crew
// builds and prepares only the harnesses of the agents in use, so with no
// Codex agent in use nothing about Codex is checked.
func (h *harness) Prepare(ctx context.Context, _ []crew.State) error {
	port.Step(ctx, "looking for codex on PATH")
	if _, err := exec.LookPath(binary); err != nil {
		return fmt.Errorf("the codex harness runs the %s CLI, which is not on PATH: %w", binary, err)
	}
	port.Step(ctx, "checking that codex is logged in")
	if os.Getenv(apiKey) != "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	out, err := h.run(ctx, proc.Command{Name: binary, Args: []string{"login", "status"}})
	switch {
	case err == nil:
		return nil
	case exitCode(err) == 1 && strings.Contains(string(out.Stderr)+string(out.Stdout), loggedOut):
		return fmt.Errorf("the codex harness runs the %s CLI, which is not logged in: run `codex login`, or set %s",
			binary, apiKey)
	}
	return fmt.Errorf("could not check that %s is logged in: %w", binary, err)
}
