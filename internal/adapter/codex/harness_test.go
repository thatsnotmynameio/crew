package codex

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// fakeProcess is a scripted codex: it prints stdout in small chunks, then
// stderr, then exits 0, or, when it hangs, runs until it is stopped.
type fakeProcess struct {
	stdout, stderr []byte
	hang           bool

	stopOnce     sync.Once
	stop         chan struct{}
	stops        int       // calls to Stop; read after Stop returns
	stopDeadline time.Time // the deadline of the first Stop's ctx
	err          error     // set before done is closed
	done         chan struct{}
}

func newProcess(stdout []byte) *fakeProcess {
	return &fakeProcess{stdout: stdout, stop: make(chan struct{}), done: make(chan struct{})}
}

func (p *fakeProcess) Wait() error {
	<-p.done
	return p.err
}

func (p *fakeProcess) Stop(ctx context.Context) error {
	p.stops++
	p.stopOnce.Do(func() {
		p.stopDeadline, _ = ctx.Deadline()
		close(p.stop)
	})
	<-p.done
	return nil
}

func (p *fakeProcess) run(stdout, stderr io.Writer) {
	for chunk := range slices.Chunk(p.stdout, 7) {
		_, _ = stdout.Write(chunk)
	}
	if len(p.stderr) > 0 {
		_, _ = stderr.Write(p.stderr)
	}
	if p.hang {
		<-p.stop
		p.err = exitError{code: -1, msg: "signal: terminated"}
	}
	close(p.done)
}

// fakeSpawn records each command and runs the scripted process.
type fakeSpawn struct {
	process  *fakeProcess
	err      error
	commands []proc.Command
}

func (f *fakeSpawn) spawn(c proc.Command, stdout, stderr io.Writer) (process, error) {
	f.commands = append(f.commands, c)
	if f.err != nil {
		return nil, f.err
	}
	go f.process.run(stdout, stderr)
	return f.process, nil
}

// fakeGit answers rev-parse with the git dirs of a linked worktree and
// records the commands it got.
type fakeGit struct {
	err      error
	commands []proc.Command
}

func (g *fakeGit) run(_ context.Context, c proc.Command) (proc.Output, error) {
	g.commands = append(g.commands, c)
	if g.err != nil {
		return proc.Output{}, g.err
	}
	return proc.Output{Stdout: []byte("/repo/.git/worktrees/issue-4-review\n/repo/.git\n")}, nil
}

// build runs the codex factory on decode, with its sessions and its git
// scripted.
func build(t *testing.T, decode port.Decode, spawn *fakeSpawn, git *fakeGit) *harness {
	t.Helper()
	built, err := Factory(&proc.Group{})(decode)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	h, ok := built.(*harness)
	if !ok {
		t.Fatalf("factory built %T, want *harness", built)
	}
	h.spawn = spawn.spawn
	h.run = git.run
	return h
}

// noSection is an agent's harness with no key but its name.
func noSection(any) error { return nil }

const worktree = "/repo/.crew/worktrees/issue-4-review"

// runSession runs one session of p to its end and returns its outcome and
// the bytes the engine's writer received.
func runSession(t *testing.T, p *fakeProcess) (crew.Outcome, []byte) {
	t.Helper()
	h := build(t, noSection, &fakeSpawn{process: p}, &fakeGit{})
	var out bytes.Buffer
	s, err := h.Start(t.Context(), port.Run{Dir: worktree, Prompt: "Review #4", Output: &out})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return s.Wait(), out.Bytes()
}

// load writes body as a repository's .crew/config.yaml and loads it, so the
// factory gets the harness section exactly as crew decodes it.
func load(t *testing.T, body string) (*config.Config, error) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".crew"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".crew", "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return config.Load(root)
}

// agent is a config of one agent on codex, whose harness has the keys
// harness, a YAML flow mapping's entries after its name, and one rule.
func agent(harness string) string {
	return `agents:
  reviewer:
    harness: {name: codex` + harness + `}
rules:
  review:
    labels: {ready: ready, running: in review, success: reviewed, failure: needs attention}
    actions:
      review: {prompt: "Review {{.Issue.Ref}}"}
`
}

func TestFactoryRefusesAKeyTheCodexHarnessDoesNotTake(t *testing.T) {
	cfg, err := load(t, agent(", effort: high"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	h, err := Factory(&proc.Group{})(cfg.Agents[0].HarnessSection)
	if err == nil || !strings.Contains(err.Error(), "agents.reviewer.harness.effort") {
		t.Errorf("factory error = %v, want one naming agents.reviewer.harness.effort", err)
	}
	if h != nil {
		t.Errorf("factory built %v, want nothing", h)
	}
}

func TestHarnessRunsTheModelOfItsSectionOrNoneWithout(t *testing.T) {
	for _, tt := range []struct{ section, model string }{
		{"", ""},
		{", model: gpt-5.5", "gpt-5.5"},
		{`, model: ""`, ""},
	} {
		cfg, err := load(t, agent(tt.section))
		if err != nil {
			t.Fatalf("Load(%q): %v", tt.section, err)
		}
		spawn := &fakeSpawn{process: newProcess(fixture(t, "success.jsonl"))}
		h := build(t, cfg.Agents[0].HarnessSection, spawn, &fakeGit{})
		s, err := h.Start(t.Context(), port.Run{Dir: worktree, Prompt: "Review #4", Output: io.Discard})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		s.Wait()

		if args := spawn.commands[0].Args; tt.model == "" && slices.Contains(args, "-m") ||
			tt.model != "" && !slices.Contains(args, tt.model) {
			t.Errorf("section %q: args = %q, want model %q", tt.section, args, tt.model)
		}
	}
}

func TestStartRunsCodexInTheWorkspaceWithItsGitDirs(t *testing.T) {
	spawn := &fakeSpawn{process: newProcess(fixture(t, "success.jsonl"))}
	git := &fakeGit{}
	h := build(t, noSection, spawn, git)

	s, err := h.Start(t.Context(), port.Run{Dir: worktree, Prompt: "Review #4", Output: io.Discard})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Wait()

	if len(git.commands) != 1 || git.commands[0].Dir != worktree || git.commands[0].Name != "git" {
		t.Errorf("git ran %#v, want one rev-parse in %s", git.commands, worktree)
	}
	want := command(port.Run{Dir: worktree, Prompt: "Review #4"}, "",
		[]string{"/repo/.git/worktrees/issue-4-review", "/repo/.git"})
	if len(spawn.commands) != 1 || !slices.Equal(spawn.commands[0].Args, want.Args) || spawn.commands[0].Dir != worktree {
		t.Errorf("spawned %#v, want %#v", spawn.commands, want)
	}
}

func TestStartFailsWithoutSpawningWhenGitCannotFindTheGitDirs(t *testing.T) {
	spawn := &fakeSpawn{process: newProcess(nil)}
	h := build(t, noSection, spawn, &fakeGit{err: errors.New("git: exit status 128: fatal: not a git repository")})

	_, err := h.Start(t.Context(), port.Run{Dir: worktree, Prompt: "Review #4", Output: io.Discard})

	if err == nil || !strings.Contains(err.Error(), worktree) {
		t.Errorf("Start = %v, want an error naming the workspace", err)
	}
	if len(spawn.commands) != 0 {
		t.Errorf("spawned %d commands, want none", len(spawn.commands))
	}
}

func TestStartWithADoneContextRunsNothing(t *testing.T) {
	spawn := &fakeSpawn{process: newProcess(nil)}
	git := &fakeGit{}
	h := build(t, noSection, spawn, git)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := h.Start(ctx, port.Run{Dir: worktree, Prompt: "Review #4", Output: io.Discard})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Start = %v, want context.Canceled", err)
	}
	if len(git.commands)+len(spawn.commands) != 0 {
		t.Errorf("ran git %d and codex %d times, want neither", len(git.commands), len(spawn.commands))
	}
}

func TestStartFailsWhenCodexCannotStart(t *testing.T) {
	notFound := errors.New(`start codex: exec: "codex": executable file not found in $PATH`)
	h := build(t, noSection, &fakeSpawn{err: notFound}, &fakeGit{})

	_, err := h.Start(t.Context(), port.Run{Dir: worktree, Prompt: "Review #4", Output: io.Discard})

	if !errors.Is(err, notFound) {
		t.Errorf("Start = %v, want %v", err, notFound)
	}
}

func TestCompletedSessionSucceedsAndTheLogGetsEverythingCodexPrinted(t *testing.T) {
	p := newProcess(fixture(t, "success.jsonl"))
	p.stderr = []byte("Reading additional input from stdin...\n")

	got, out := runSession(t, p)

	if want := (crew.Outcome{Succeeded: true, Reason: "I fixed the parser. The tests pass."}); got != want {
		t.Errorf("outcome = %+v, want %+v", got, want)
	}
	if want := slices.Concat(p.stdout, p.stderr); !bytes.Equal(out, want) {
		t.Errorf("writer got %d bytes, want stdout then stderr, %d bytes", len(out), len(want))
	}
}

func TestSessionWhoseTurnFailedFailsWithItsErrorThoughCodexExited0(t *testing.T) {
	got, _ := runSession(t, newProcess(fixture(t, "failed.jsonl")))

	if got.Succeeded || !strings.HasPrefix(got.Reason, "stream disconnected before completion") {
		t.Errorf("outcome = %+v, want the turn's error", got)
	}
}

func TestWaitGivesEveryCallerTheSameOutcome(t *testing.T) {
	h := build(t, noSection, &fakeSpawn{process: newProcess(fixture(t, "success.jsonl"))}, &fakeGit{})
	s, err := h.Start(t.Context(), port.Run{Dir: worktree, Prompt: "Review #4", Output: io.Discard})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	var wg sync.WaitGroup
	outcomes := make([]crew.Outcome, 2)
	for i := range outcomes {
		wg.Go(func() { outcomes[i] = s.Wait() })
	}
	wg.Wait()

	if outcomes[0] != outcomes[1] || !outcomes[0].Succeeded {
		t.Errorf("outcomes = %+v, want the same success twice", outcomes)
	}
}

// start starts a session of p and returns it.
func start(t *testing.T, p *fakeProcess) port.Session {
	t.Helper()
	h := build(t, noSection, &fakeSpawn{process: p}, &fakeGit{})
	s, err := h.Start(t.Context(), port.Run{Dir: worktree, Prompt: "Review #4", Output: io.Discard})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return s
}

// usageOf returns what s reports it used, failing when it reports nothing.
func usageOf(t *testing.T, s port.Session) crew.Usage {
	t.Helper()
	r, ok := s.(port.UsageReporter)
	if !ok {
		t.Fatalf("session %T is not a port.UsageReporter", s)
	}
	return r.Usage()
}

func TestFinishedSessionReportsItsTokensAndTurnsButNoCost(t *testing.T) {
	s := start(t, newProcess(fixture(t, "success.jsonl")))
	s.Wait()

	got := usageOf(t, s)

	want := crew.Usage{Tokens: crew.Tokens{Input: 315, Output: 122, CacheRead: 24448}, HasTokens: true, Turns: 1, HasTurns: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

// A session crew stopped reports nothing, though its turn had completed
// before codex was stopped.
func TestStoppedSessionReportsNoUsage(t *testing.T) {
	p := newProcess(fixture(t, "success.jsonl"))
	p.hang = true
	s := start(t, p)
	if err := s.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := usageOf(t, s); !reflect.DeepEqual(got, crew.Usage{}) {
		t.Errorf("usage = %+v, want nothing", got)
	}
}

// saidBy returns what s last said, failing when it cannot tell.
func saidBy(t *testing.T, s port.Session) string {
	t.Helper()
	n, ok := s.(port.Narrator)
	if !ok {
		t.Fatalf("session %T is not a port.Narrator", s)
	}
	return n.Said()
}

// The engine reads what a session said from its own goroutine while codex
// still prints, and again once the session was stopped.
func TestSaidIsTheLastMessageWhileCodexRunsAndAfterItWasStopped(t *testing.T) {
	p := newProcess(fixture(t, "success.jsonl"))
	p.hang = true
	s := start(t, p)

	const want = "I fixed the parser. The tests pass."
	for saidBy(t, s) != want {
		time.Sleep(time.Millisecond) // codex is still printing
	}
	if err := s.Stop(t.Context()); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := saidBy(t, s); got != want {
		t.Errorf("said after the stop = %q, want %q", got, want)
	}
}

// failingWriter is a log that cannot be written, such as on a full disk.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestAFailingLogNeitherStopsTheOutputNorChangesTheVerdict(t *testing.T) {
	h := build(t, noSection, &fakeSpawn{process: newProcess(fixture(t, "success.jsonl"))}, &fakeGit{})
	s, err := h.Start(t.Context(), port.Run{Dir: worktree, Prompt: "Review #4", Output: failingWriter{}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := s.Wait(); !got.Succeeded {
		t.Errorf("outcome = %+v, want success", got)
	}
}

func TestStopEndsTheSessionWithinTheCallersDeadlineAsAFailure(t *testing.T) {
	p := newProcess(fixture(t, "interrupted.jsonl"))
	p.hang = true
	h := build(t, noSection, &fakeSpawn{process: p}, &fakeGit{})
	s, err := h.Start(t.Context(), port.Run{Dir: worktree, Prompt: "Review #4", Output: io.Discard})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}

	if p.stops == 0 || !p.stopDeadline.Equal(deadline) {
		t.Errorf("process stopped %d times with deadline %v, want the caller's %v", p.stops, p.stopDeadline, deadline)
	}
	if got := s.Wait(); got != (crew.Outcome{Reason: "stopped by crew before the session ended"}) {
		t.Errorf("outcome = %+v, want a failure saying crew stopped it", got)
	}
}

// Stopping a session that already ended does nothing: its outcome stays
// the one codex earned, and its process is not signalled, whether or not the
// engine has waited on it yet.
func TestStopAfterTheSessionEndedChangesNothing(t *testing.T) {
	for _, waited := range []bool{true, false} {
		p := newProcess(fixture(t, "success.jsonl"))
		h := build(t, noSection, &fakeSpawn{process: p}, &fakeGit{})
		s, err := h.Start(t.Context(), port.Run{Dir: worktree, Prompt: "Review #4", Output: io.Discard})
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		if waited {
			s.Wait()
		} else if ended, ok := s.(*session); ok {
			<-ended.done // reaped and judged, though nobody waited
		}

		if err := s.Stop(t.Context()); err != nil {
			t.Fatalf("Stop: %v", err)
		}

		if got := s.Wait(); !got.Succeeded {
			t.Errorf("waited first %v: outcome = %+v, want the success codex earned", waited, got)
		}
		if p.stops != 0 {
			t.Errorf("waited first %v: the ended process got %d stops, want none", waited, p.stops)
		}
	}
}

// TestFactorySessionRunsCodexThroughTheGroup runs a stand-in codex script
// through a real process group in a real worktree: the production path from
// the factory through git and proc, with stdout teed and stderr in the log.
func TestFactorySessionRunsCodexThroughTheGroup(t *testing.T) {
	_, dir := repository(t)
	bin := t.TempDir()
	fixturePath, err := filepath.Abs(filepath.Join("testdata", "success.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	argv := filepath.Join(bin, "argv")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argv + "'\n" +
		"echo \"codex ran in $(pwd -P)\" >&2\ncat '" + fixturePath + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	h, err := Factory(&proc.Group{})(noSection)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	var out bytes.Buffer
	run := port.Run{Dir: dir, Prompt: "Review #4", Output: &out}
	s, err := h.Start(t.Context(), run)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := s.Wait(); !got.Succeeded {
		t.Errorf("outcome = %+v, want success", got)
	}
	if !strings.Contains(out.String(), "codex ran in "+dir) || !strings.Contains(out.String(), `"turn.completed"`) {
		t.Errorf("log = %q, want codex's stderr from %s and its events", out.String(), dir)
	}
	dirs, err := gitDirs(t.Context(), (&proc.Group{}).Run, dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(argv)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Join(command(run, "", dirs).Args, "\n") + "\n"; string(got) != want {
		t.Errorf("codex got args\n%s\nwant\n%s", got, want)
	}
}
