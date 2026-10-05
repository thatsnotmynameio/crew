package shell_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/adapter/shell"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// output is a writer safe for the check's copy goroutine and the test.
type output struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n, _ := o.buf.Write(p) // a bytes.Buffer's Write never returns an error
	return n, nil
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.buf.String()
}

// check returns a check of command in a new directory, for issue #14.
func check(t *testing.T, command string, out *output) port.Check {
	t.Helper()
	return port.Check{
		Dir: t.TempDir(), Command: command,
		IssueRef: "#14", IssueKey: "14", IssueURL: "https://github.com/o/r/issues/14",
		Branch: "crew/issue-14-lfg", Output: out,
	}
}

// withoutCrewEnv clears every CREW_ variable crew's own environment holds,
// such as those of a crew session running these tests, until the test
// ends, so a check sees only the ones the checker sets.
func withoutCrewEnv(t *testing.T) {
	t.Helper()
	for _, e := range os.Environ() {
		if name, _, _ := strings.Cut(e, "="); strings.HasPrefix(name, "CREW_") {
			t.Setenv(name, "") // restores the variable when the test ends
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestCheckThatExitsZeroPasses(t *testing.T) {
	var out output
	if err := shell.New(&proc.Group{}).Check(context.Background(), check(t, "true", &out)); err != nil {
		t.Fatalf("Check = %v, want nil", err)
	}
}

func TestCheckThatExitsNonZeroFailsWithItsOutputInOrder(t *testing.T) {
	var out output
	err := shell.New(&proc.Group{}).Check(context.Background(), check(t, "echo one; echo two >&2; exit 3", &out))
	if !errors.Is(err, port.ErrCheckFailed) {
		t.Fatalf("Check = %v, want an error wrapping ErrCheckFailed", err)
	}
	if got := out.String(); got != "one\ntwo\n" {
		t.Errorf("output = %q, want both lines in order", got)
	}
}

func TestCheckReadsTheIssueFromItsEnvironmentInItsDirectory(t *testing.T) {
	var out output
	command := `printf '%s|%s|%s|%s|%s\n' ` +
		`"$CREW_ISSUE_REF" "$CREW_ISSUE_KEY" "$CREW_ISSUE_URL" "$CREW_BRANCH" "$(pwd -P)"`
	c := check(t, command, &out)
	if err := shell.New(&proc.Group{}).Check(context.Background(), c); err != nil {
		t.Fatalf("Check: %v", err)
	}
	dir, err := filepath.EvalSymlinks(c.Dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "#14|14|https://github.com/o/r/issues/14|crew/issue-14-lfg|" + dir + "\n"
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestAE6CheckRunsOnlyItsCommandWhateverTheIssueTitle(t *testing.T) {
	// The title is not part of port.Check at all, so it cannot reach the
	// command: the check sees only crew's six variables, and the command
	// runs as written.
	withoutCrewEnv(t)
	var out output
	c := check(t, `env | grep '^CREW_' | sort`, &out)
	if err := shell.New(&proc.Group{}).Check(context.Background(), c); err != nil {
		t.Fatalf("Check: %v", err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		name, _, _ := strings.Cut(line, "=")
		switch name {
		case "CREW_BOTS", "CREW_BRANCH", "CREW_CODE_OWNERS", "CREW_ISSUE_KEY", "CREW_ISSUE_REF", "CREW_ISSUE_URL":
		default:
			t.Errorf("unexpected variable %q", line)
		}
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "pwned")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pwned exists: %v", err)
	}
}

// AE6 of #80: whatever GH_TOKEN your shell exports, the check's gh
// reads its bot's directory, and the check learns the code owners and the bots.
func TestCheckActsAsItsIdentityAndNamesTheCodeOwnersAndTheBots(t *testing.T) {
	withoutCrewEnv(t)
	t.Setenv("GH_TOKEN", "your-token")
	var out output
	c := check(t, `echo "$GH_CONFIG_DIR|$CREW_CODE_OWNERS|$CREW_BOTS|${GH_TOKEN-unset}"`+
		`"|${CREW_BOSS-unset}|${CREW_MATES-unset}"`, &out)
	c.Identity = port.Identity{
		Bot: "developer", Login: "crew-developer[bot]",
		Env:   []string{"GH_CONFIG_DIR=/run/crew/developer"},
		Unset: []string{"GH_TOKEN", "GITHUB_TOKEN"},
	}
	c.CodeOwners = []string{"octocat"}
	c.Bots = []string{"crew-developer[bot]", "crew-ops[bot]"}

	if err := shell.New(&proc.Group{}).Check(context.Background(), c); err != nil {
		t.Fatalf("Check: %v", err)
	}
	want := "/run/crew/developer|octocat|crew-developer[bot] crew-ops[bot]|unset|unset|unset\n"
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestCheckEndedByItsContextIsKilledWithWhatItStarted(t *testing.T) {
	var out output
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	c := check(t, `sleep 60 & echo $! > child; wait`, &out)

	start := time.Now()
	err := shell.New(&proc.Group{}).Check(ctx, c)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, port.ErrCheckFailed) {
		t.Fatalf("Check = %v, want an error wrapping the context's", err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("Check returned after %v, want soon after the context ended", took)
	}
	pid, err := os.ReadFile(filepath.Join(c.Dir, "child"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(pid)))
	if err != nil {
		t.Fatal(err)
	}
	// A killed child stays a zombie until init reaps it, so give that a
	// moment.
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(n, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("the check's child %d still runs", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCheckThatCannotStartIsNotAFailedCheck(t *testing.T) {
	var out output
	c := check(t, "true", &out)
	c.Dir = filepath.Join(c.Dir, "missing")
	err := shell.New(&proc.Group{}).Check(context.Background(), c)
	if err == nil || errors.Is(err, port.ErrCheckFailed) {
		t.Fatalf("Check = %v, want a start error", err)
	}
}
