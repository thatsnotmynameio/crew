package claude

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// exitError is a fake process's non-zero exit, read as *exec.ExitError is.
type exitError struct {
	code int
	msg  string
}

func (e exitError) Error() string { return e.msg }
func (e exitError) ExitCode() int { return e.code }

// fakeProcess is a scripted claude, after the old dispatcher's FakeProcess:
// it prints stdout in small chunks, then stderr, then exits with exit, or,
// when it hangs, runs until it is stopped.
type fakeProcess struct {
	stdout, stderr []byte
	exit           error
	hang           bool

	stopOnce sync.Once
	stop     chan struct{}
	stopCtx  context.Context // the ctx Stop got; read after Stop returns
	err      error           // set before done is closed
	done     chan struct{}
}

func newProcess(stdout []byte, exit error) *fakeProcess {
	return &fakeProcess{stdout: stdout, exit: exit, stop: make(chan struct{}), done: make(chan struct{})}
}

func (p *fakeProcess) run(stdout, stderr io.Writer) {
	// Chunks that split lines prove the parser reads a stream, not lines.
	for chunk := range slices.Chunk(p.stdout, 7) {
		_, _ = stdout.Write(chunk)
	}
	if len(p.stderr) > 0 {
		_, _ = stderr.Write(p.stderr)
	}
	p.err = p.exit
	if p.hang {
		<-p.stop
		p.err = exitError{code: -1, msg: "signal: terminated"}
	}
	close(p.done)
}

func (p *fakeProcess) Wait() error {
	<-p.done
	return p.err
}

func (p *fakeProcess) Stop(ctx context.Context) error {
	p.stopOnce.Do(func() {
		p.stopCtx = ctx
		close(p.stop)
	})
	<-p.done
	return nil
}

// fakeSpawn is the old dispatcher's FakeSpawn: it records each command and
// runs the scripted process.
type fakeSpawn struct {
	process  *fakeProcess
	commands []proc.Command
}

func (f *fakeSpawn) spawn(c proc.Command, stdout, stderr io.Writer) (process, error) {
	f.commands = append(f.commands, c)
	go f.process.run(stdout, stderr)
	return f.process, nil
}

// build runs the claude factory on decode, with its sessions scripted by
// spawn.
func build(t *testing.T, decode port.Decode, spawn *fakeSpawn) *harness {
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
	return h
}

// noSection is a config with neither config.model nor harness:.
func noSection(any) error { return nil }

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// runSession runs one session of p to its end and returns its outcome and the
// bytes the engine's writer received.
func runSession(t *testing.T, p *fakeProcess) (crew.Outcome, []byte) {
	t.Helper()
	h := build(t, noSection, &fakeSpawn{process: p})
	var out bytes.Buffer
	s, err := h.Start(t.Context(), port.Run{Dir: "/work", Prompt: "Implement #4", Output: &out})
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

const workflow = `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`

func TestCommandRunsClaudeHeadlessWithTheModelInTheDirectory(t *testing.T) {
	got := command("Implement #4", "claude-opus-5-5", "/work/.crew/worktrees/issue-4-development")

	want := proc.Command{
		Name: "claude",
		Args: []string{
			"-p",
			"--model", "claude-opus-5-5",
			"--permission-mode", "auto",
			"--output-format", "stream-json",
			"--verbose",
			"--", "Implement #4",
		},
		Dir: "/work/.crew/worktrees/issue-4-development",
	}
	if got.Name != want.Name || got.Dir != want.Dir || !slices.Equal(got.Args, want.Args) || got.Env != nil {
		t.Errorf("command = %#v, want %#v", got, want)
	}
}

func TestCommandPassesAPromptStartingWithADashAsThePrompt(t *testing.T) {
	prompt := "- Read the issue\n--dry-run does nothing"

	args := command(prompt, "claude-opus-5-5", "/work").Args

	if n := len(args); n < 2 || args[n-2] != "--" || args[n-1] != prompt {
		t.Errorf("args = %q, want the prompt last, right after --", args)
	}
}

func TestFactoryRunsTheConfiguredModelAndDefaultsToOpus(t *testing.T) {
	for _, tc := range []struct {
		name, config, want string
	}{
		{"no model", workflow, "claude-opus-5-5"},
		{"empty model", "config:\n  model: \"\"\n" + workflow, "claude-opus-5-5"},
		{"configured model", "config:\n  model: claude-sonnet-5\n" + workflow, "claude-sonnet-5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := load(t, tc.config)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			spawn := &fakeSpawn{process: newProcess(fixture(t, "success.jsonl"), nil)}
			h := build(t, cfg.HarnessSection, spawn)

			s, err := h.Start(t.Context(), port.Run{Dir: "/work", Prompt: "Implement #4", Output: io.Discard})
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			s.Wait()

			if len(spawn.commands) != 1 {
				t.Fatalf("spawned %d commands, want 1", len(spawn.commands))
			}
			args := spawn.commands[0].Args
			if i := slices.Index(args, "--model"); i < 0 || i+1 >= len(args) || args[i+1] != tc.want {
				t.Errorf("args = %q, want --model %s", args, tc.want)
			}
		})
	}
}

func TestFactoryRejectsAnUnknownHarnessKeyNamingIt(t *testing.T) {
	cfg, err := load(t, "harness:\n  effort: high\n"+workflow)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	h, err := Factory(&proc.Group{})(cfg.HarnessSection)
	if err == nil || !strings.Contains(err.Error(), "harness.effort") {
		t.Errorf("factory error = %v, want one naming harness.effort", err)
	}
	if h != nil {
		t.Errorf("factory built %v, want nothing", h)
	}
}

func TestCleanResultSucceedsWithItsText(t *testing.T) {
	got, _ := runSession(t, newProcess(fixture(t, "success.jsonl"), nil))

	want := crew.Outcome{Succeeded: true, Reason: "Opened pull request #12 for issue #4. The tests pass."}
	if got != want {
		t.Errorf("outcome = %+v, want %+v", got, want)
	}
}

func TestErrorResultFailsWithItsTextOnOneLineCutTo200Characters(t *testing.T) {
	got, _ := runSession(t, newProcess(fixture(t, "error.jsonl"), exitError{code: 1, msg: "exit status 1"}))

	want := crew.Outcome{Reason: `API Error: 529 {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}} ` +
		`The request could not be completed because the service is temporarily overloaded. The session stopped before…`}
	if got != want {
		t.Errorf("outcome = %+v, want %+v", got, want)
	}
	if n := utf8.RuneCountInString(got.Reason); n != 200 {
		t.Errorf("reason has %d characters, want 200", n)
	}
}

func TestNoResultFailsWithTheExitCode(t *testing.T) {
	got, _ := runSession(t, newProcess(fixture(t, "noresult.jsonl"), exitError{code: 1, msg: "exit status 1"}))

	want := crew.Outcome{Reason: "exit code 1"}
	if got != want {
		t.Errorf("outcome = %+v, want %+v", got, want)
	}
}

func TestCleanResultFailsWhenTheProcessExitsNonZero(t *testing.T) {
	got, _ := runSession(t, newProcess(fixture(t, "success.jsonl"), exitError{code: 2, msg: "exit status 2"}))

	if got.Succeeded || !strings.Contains(got.Reason, "exit code 2") {
		t.Errorf("outcome = %+v, want a failure naming exit code 2", got)
	}
}

func TestQuotedIsErrorInsideAMessageStillSucceeds(t *testing.T) {
	got, _ := runSession(t, newProcess(fixture(t, "quoted.jsonl"), nil))

	if !got.Succeeded {
		t.Errorf("outcome = %+v, want success", got)
	}
}

func TestEngineWriterGetsTheStreamByteForByte(t *testing.T) {
	want := fixture(t, "success.jsonl")
	_, got := runSession(t, newProcess(want, nil))

	if !bytes.Equal(got, want) {
		t.Errorf("writer got %d bytes, want the fixture's %d byte for byte", len(got), len(want))
	}
}

// failingWriter is a log that cannot be written, such as on a full disk.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestAFailingLogNeitherStopsTheStreamNorChangesTheVerdict(t *testing.T) {
	h := build(t, noSection, &fakeSpawn{process: newProcess(fixture(t, "success.jsonl"), nil)})
	s, err := h.Start(t.Context(), port.Run{Dir: "/work", Prompt: "Implement #4", Output: failingWriter{}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := s.Wait(); !got.Succeeded {
		t.Errorf("outcome = %+v, want success", got)
	}
}

func TestStopEndsTheSessionWithinTheCallersDeadlineAsAFailure(t *testing.T) {
	p := newProcess(fixture(t, "noresult.jsonl"), nil)
	p.hang = true
	h := build(t, noSection, &fakeSpawn{process: p})
	s, err := h.Start(t.Context(), port.Run{Dir: "/work", Prompt: "Implement #4", Output: io.Discard})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	ctx, cancel := context.WithDeadline(t.Context(), deadline)
	defer cancel()
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if p.stopCtx == nil {
		t.Fatal("the process was never stopped")
	}
	if got, ok := p.stopCtx.Deadline(); !ok || !got.Equal(deadline) {
		t.Errorf("process stopped with deadline %v, want the caller's %v", got, deadline)
	}
	if got := s.Wait(); got.Succeeded || !strings.Contains(got.Reason, "stopped") {
		t.Errorf("outcome = %+v, want a failure saying it was stopped", got)
	}
}

func TestPreparerFailsNamingClaudeWhenItIsNotOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	h, err := Factory(&proc.Group{})(noSection)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	err = port.Prepare(t.Context(), []crew.State{"ready"}, h)
	if err == nil || !strings.Contains(err.Error(), "claude") {
		t.Errorf("Prepare = %v, want an error naming claude", err)
	}
}

// TestFactorySessionRunsClaudeThroughTheGroup runs a stand-in claude script
// through a real process group: the production path from the factory to
// proc, with stdout teed and stderr in the same log.
func TestFactorySessionRunsClaudeThroughTheGroup(t *testing.T) {
	bin := t.TempDir()
	fixturePath, err := filepath.Abs(filepath.Join("testdata", "success.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho \"claude ran in $(pwd -P)\" >&2\ncat '" + fixturePath + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	h, err := Factory(&proc.Group{})(noSection)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if err := port.Prepare(t.Context(), []crew.State{"ready"}, h); err != nil {
		t.Fatalf("Prepare with claude on PATH: %v", err)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	s, err := h.Start(t.Context(), port.Run{Dir: dir, Prompt: "Implement #4", Output: &out})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if got := s.Wait(); !got.Succeeded {
		t.Errorf("outcome = %+v, want success", got)
	}
	stderr := "claude ran in " + dir + "\n"
	log := out.String()
	if !strings.Contains(log, stderr) {
		t.Errorf("log %q does not hold stderr %q", log, stderr)
	}
	if got := strings.Replace(log, stderr, "", 1); got != string(fixture(t, "success.jsonl")) {
		t.Errorf("log without stderr = %q, want the stream as printed", got)
	}
}
