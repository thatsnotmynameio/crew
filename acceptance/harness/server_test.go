package harness

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
)

// ran is how a double run by a test ended.
type ran struct {
	stdout, stderr string
	code           int
}

// double returns the command that runs the double name from s's bin
// directory with args, in a new working directory, with only the
// environment a scenario gives it: the socket, the variables the doubles
// forward, and GORACE without the race detector's one-second sleep at
// every successful exit of a -race test binary.
func double(t *testing.T, s *Server, name string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), filepath.Join(s.Bin(), name), args...)
	cmd.Dir = t.TempDir()
	cmd.Env = []string{SocketEnv + "=" + s.Socket(), "GH_CONFIG_DIR=/gh", "CREW_BOTS=crew-developer[bot]",
		"HOME=/nowhere", "GORACE=atexit_sleep_ms=0"}
	return cmd
}

// run runs cmd, with stdin as its standard input when it is not "", and
// returns how it ended.
func run(t *testing.T, cmd *exec.Cmd, stdin string) ran {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	err := cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("run %s: %v", cmd.Path, err)
	}
	return ran{stdout: stdout.String(), stderr: stderr.String(), code: cmd.ProcessState.ExitCode()}
}

// claudeArgs is the headless command line of claude with prompt.
func claudeArgs(prompt string) []string {
	return []string{"-p", "--model", "claude-opus-5-5", "--permission-mode", "auto",
		"--output-format", "stream-json", "--verbose", "--", prompt}
}

// scenario is a testing.TB for a scenario under test: the screen tests'
// recorder, which records failures, that also records the cleanups the
// scenario registers, so a test can run them and check what they report.
type scenario struct {
	*recorder

	cleanups []func()
}

// newScenario returns a scenario that has recorded nothing.
func newScenario() *scenario {
	return &scenario{recorder: &recorder{}}
}

// Cleanup records f, for finish to run.
func (r *scenario) Cleanup(f func()) {
	r.cleanups = append(r.cleanups, f)
}

// finish runs the cleanups last-in first-out, as the testing package does,
// and returns the failures recorded.
func (r *scenario) finish() []string {
	for _, f := range slices.Backward(r.cleanups) {
		f()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.msgs
}

// stdinGH is a GH that records the standard input of each call and prints
// it back.
type stdinGH struct {
	mu  sync.Mutex
	got [][]byte
}

// Run implements GH.
func (g *stdinGH) Run(inv fakegithub.Invocation) fakegithub.Reply {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.got = append(g.got, inv.Stdin)
	return fakegithub.Reply{Stdout: inv.Stdin}
}

// repoViewGH is the fake GitHub taught gh repo view, as a developer teaches
// the fake a call crew starts making.
type repoViewGH struct{ *fakegithub.GitHub }

// Run answers gh repo view and hands every other call to the fake.
func (g repoViewGH) Run(inv fakegithub.Invocation) fakegithub.Reply {
	if len(inv.Args) >= 2 && inv.Args[0] == "repo" && inv.Args[1] == "view" {
		return fakegithub.Reply{Stdout: []byte("widgets\n")}
	}
	return g.GitHub.Run(inv)
}

// Covers U3: the gh double, run through its link with a scenario's
// environment, answers from the fake GitHub.
func TestTheGHDoubleAnswersFromTheFakeGitHub(t *testing.T) {
	s := Serve(t, fakegithub.New("acme", "widgets"), nil)

	got := run(t, double(t, s, "gh", "api", "user", "--jq", ".login"), "")

	if got.code != 0 || got.stdout != "boss\n" || got.stderr != "" {
		t.Fatalf("gh api user = %+v, want boss and exit 0", got)
	}
}

// Covers U3: the gh double carries its standard input, its arguments and
// the forwarded environment variables to the fake.
func TestTheGHDoubleCarriesStdinToTheFake(t *testing.T) {
	gh := &stdinGH{}
	s := Serve(t, gh, nil)

	got := run(t, double(t, s, "gh", "api", "graphql", "--input", "-"), `{"query":"{viewer{login}}"}`)

	if got.code != 0 || got.stdout != `{"query":"{viewer{login}}"}` {
		t.Fatalf("gh with stdin = %+v, want its stdin printed back", got)
	}
	if len(gh.got) != 1 || string(gh.got[0]) != `{"query":"{viewer{login}}"}` {
		t.Fatalf("the fake received stdin %q", gh.got)
	}
}

// Covers AE4 (R13, R14): a gh call the fake GitHub does not know fails the
// scenario at cleanup, naming the call; once the fake is taught the call,
// the same run passes. The teaching here wraps the fake in a GH that knows
// gh repo view, as a developer adds the call to the fake.
func TestAnUnknownGHCallFailsTheScenarioUntilTheFakeLearnsIt(t *testing.T) {
	unknown := newScenario()
	s := Serve(unknown, fakegithub.New("acme", "widgets"), nil)
	got := run(t, double(t, s, "gh", "repo", "view", "--json", "name"), "")
	if got.code != 1 || !strings.Contains(got.stderr, "gh repo view --json name") {
		t.Fatalf("unknown call = %+v, want exit 1 naming the call", got)
	}
	if err := s.Err(); err == nil || !strings.Contains(err.Error(), "gh repo view --json name") {
		t.Fatalf("Err() = %v, want the call named", err)
	}
	failures := unknown.finish()
	if len(failures) != 1 || !strings.Contains(failures[0], "gh repo view --json name (unknown command)") {
		t.Fatalf("failures = %q, want one naming gh repo view --json name", failures)
	}

	taught := newScenario()
	s = Serve(taught, repoViewGH{fakegithub.New("acme", "widgets")}, nil)
	got = run(t, double(t, s, "gh", "repo", "view", "--json", "name"), "")
	if got.code != 0 || got.stdout != "widgets\n" {
		t.Fatalf("taught call = %+v, want widgets and exit 0", got)
	}
	if failures := taught.finish(); len(failures) != 0 {
		t.Fatalf("failures after teaching the fake = %q, want none", failures)
	}
}

// Covers U3: the claude double prints the matching script's events in
// order and exits with its code; a second invocation with the same key
// takes the next script, and the script sees the double's working
// directory and environment.
func TestTheClaudeDoubleRunsScriptsInOrder(t *testing.T) {
	c := fakeclaude.New(nil)
	var dir, bots string
	c.Script("issue 4", func(_ context.Context, s *fakeclaude.Session) int {
		dir, bots = s.Dir, s.Env["CREW_BOTS"]
		_ = s.Emit(s.Init(), s.Said("first"), s.Failure("first failed"))
		return 1
	})
	c.Script("issue 4", fakeclaude.Succeed("second"))
	s := Serve(t, nil, c)

	cmd := double(t, s, "claude", claudeArgs("Work on issue 4.")...)
	got := run(t, cmd, "")
	if got.code != 1 || !inOrder(got.stdout, `"subtype":"init"`, `"text":"first"`, `"result":"first failed"`) {
		t.Fatalf("first claude = %+v, want init, first, the failure and exit 1", got)
	}
	if dir != cmd.Dir || bots != "crew-developer[bot]" {
		t.Fatalf("script saw dir %q and CREW_BOTS %q, want %q and crew-developer[bot]", dir, bots, cmd.Dir)
	}
	got = run(t, double(t, s, "claude", claudeArgs("Work on issue 4.")...), "")
	if got.code != 0 || !strings.Contains(got.stdout, `"result":"second"`) {
		t.Fatalf("second claude = %+v, want the second script's success", got)
	}
}

// inOrder reports whether s holds each of parts, in that order.
func inOrder(s string, parts ...string) bool {
	for _, p := range parts {
		i := strings.Index(s, p)
		if i < 0 {
			return false
		}
		s = s[i+len(p):]
	}
	return true
}

// Covers U3 (KTD6): a claude invocation no script matches is journaled as a
// violation naming the prompt's first 200 characters, and exits 1.
func TestAnUnscriptedClaudeIsAViolation(t *testing.T) {
	rec := newScenario()
	s := Serve(rec, nil, fakeclaude.New(nil))
	prompt := strings.Repeat("p", 200) + "-tail"

	got := run(t, double(t, s, "claude", claudeArgs(prompt)...), "")

	if got.code != 1 {
		t.Fatalf("unscripted claude = %+v, want exit 1", got)
	}
	failures := rec.finish()
	if len(failures) != 1 || !strings.Contains(failures[0], strings.Repeat("p", 200)+"…(205 chars)") ||
		strings.Contains(failures[0], "-tail") {
		t.Fatalf("failures = %q, want one naming the prompt's first 200 characters", failures)
	}
}

// Covers U3 (KTD3): a script that blocks keeps the claude double running;
// SIGTERM to the double's process group ends it within a second, with exit
// code 143, and cancels the script's context.
func TestSIGTERMEndsABlockedClaudeAndCancelsItsScript(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	c := fakeclaude.New(nil)
	c.Script("block", func(ctx context.Context, s *fakeclaude.Session) int {
		_ = s.Emit(s.Init())
		close(started)
		<-ctx.Done()
		close(cancelled)
		return 1
	})
	s := Serve(t, nil, c)
	cmd := double(t, s, "claude", claudeArgs("block")...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	<-started
	select {
	case err := <-exited:
		t.Fatalf("the double exited while its script blocks: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("the double still runs a second after SIGTERM")
	}
	if code := cmd.ProcessState.ExitCode(); code != exitTerminated {
		t.Fatalf("exit code = %d, want %d", code, exitTerminated)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("the script's context was not cancelled a second after SIGTERM")
	}
}

// Covers U3 (KTD3): a pull request a script opens through its GitHub handle
// is visible to a later gh pr list --head.
func TestAPullRequestAScriptOpensIsVisibleToGH(t *testing.T) {
	gh := fakegithub.New("acme", "widgets")
	c := fakeclaude.New(gh)
	c.Script("open a pull request", func(_ context.Context, s *fakeclaude.Session) int {
		n := s.GitHub.AddPullRequest(fakegithub.PullRequest{Title: "Fix it", HeadBranch: "fix-it"})
		_ = s.Emit(s.Init(), s.Success(fmt.Sprintf("Opened pull request #%d.", n)))
		return 0
	})
	s := Serve(t, gh, c)
	if got := run(t, double(t, s, "claude", claudeArgs("Please open a pull request.")...), ""); got.code != 0 {
		t.Fatalf("claude = %+v, want exit 0", got)
	}

	got := run(t, double(t, s, "gh", "pr", "list", "--head", "fix-it", "--json", "number,title", "--jq", ".[].title"), "")

	if got.code != 0 || got.stdout != "Fix it\n" {
		t.Fatalf("gh pr list --head fix-it = %+v, want the script's pull request", got)
	}
}

// Covers U3 (KTD3): a double exits non-zero and says why when the socket's
// variable is unset or the socket is gone.
func TestADoubleWithoutTheServerFailsClearly(t *testing.T) {
	s, err := NewServer(fakegithub.New("acme", "widgets"), nil)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := link(bin); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(bin, "gh")
	socket := s.Socket()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	unsetCmd := exec.CommandContext(t.Context(), exe, "api", "user")
	unsetCmd.Env = []string{}
	unset := run(t, unsetCmd, "")
	goneCmd := exec.CommandContext(t.Context(), exe, "api", "user")
	goneCmd.Env = []string{SocketEnv + "=" + socket}
	gone := run(t, goneCmd, "")

	if unset.code == 0 || !strings.Contains(unset.stderr, SocketEnv+" is not set") {
		t.Fatalf("without %s: %+v, want a failure naming it", SocketEnv, unset)
	}
	if gone.code == 0 || !strings.Contains(gone.stderr, "cannot reach the test process at "+socket) {
		t.Fatalf("with the socket gone: %+v, want a failure naming it", gone)
	}
}
