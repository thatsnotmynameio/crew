package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"

	"github.com/thatsnotmynameio/crew/acceptance/fakeclaude"
	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
)

// SocketEnv is the environment variable that tells the gh and claude
// doubles where the Server listens.
const SocketEnv = "ACCEPTANCE_DOUBLES_SOCKET"

// The names the test binary plays the doubles under, from the symbolic
// links in Server.Bin.
const (
	ghName     = "gh"
	claudeName = "claude"
)

// binPerm is the mode of the doubles' bin directory.
const binPerm = 0o700

// GH answers gh calls. *fakegithub.GitHub is one; a test may wrap it.
type GH interface {
	Run(inv fakegithub.Invocation) fakegithub.Reply
}

// request is what a double sends the server: one invocation.
type request struct {
	// Name is the program the double plays: gh or claude.
	Name string `json:"name"`
	// Args is its argument list, without the program's name.
	Args []string `json:"args"`
	// Dir is its working directory.
	Dir string `json:"dir"`
	// Stdin is what its standard input held (gh only).
	Stdin []byte `json:"stdin,omitempty"`
	// Env holds GH_CONFIG_DIR and the CREW_* variables it was given.
	Env map[string]string `json:"env"`
	// Pid is the double's process id, so teardown can kill it.
	Pid int `json:"pid"`
}

// frame is one line the server sends a double: a chunk of standard output,
// a chunk of standard error, the order to ignore SIGTERM from then on, or the
// exit code, which is the last frame.
type frame struct {
	Stdout     []byte `json:"stdout,omitempty"`
	Stderr     []byte `json:"stderr,omitempty"`
	IgnoreTerm bool   `json:"ignore_term,omitempty"`
	Exit       *int   `json:"exit,omitempty"`
}

// Server is the test process's end of the doubles: it listens on a Unix
// socket, answers each gh invocation from a GH and each claude invocation
// from a fake Claude Code, and journals the violations, the calls neither
// knows. Build it with NewServer or Serve.
type Server struct {
	gh     GH
	claude *fakeclaude.Claude
	dir    string
	ln     net.Listener
	ctx    context.Context //nolint:containedctx // the server's lifetime; every invocation's context derives from it
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu         sync.Mutex
	violations []string
	pids       map[int]int // invocations in flight, by double's pid
	conns      map[net.Conn]struct{}
}

// NewServer starts a server that answers gh from gh and claude from claude;
// either may be nil, which makes every call to that program a violation. It
// listens in a new short temporary directory, which also holds the bin
// directory of the doubles. Close stops it and removes the directory.
func NewServer(gh GH, claude *fakeclaude.Claude) (*Server, error) {
	// Linux caps a socket's path at 108 bytes, so the directory is short
	// and directly under the system's temporary directory.
	dir, err := os.MkdirTemp("", "cf")
	if err != nil {
		return nil, fmt.Errorf("make the doubles' directory: %w", err)
	}
	if err := link(filepath.Join(dir, "bin")); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	ln, err := (&net.ListenConfig{}).Listen(ctx, "unix", filepath.Join(dir, "s"))
	if err != nil {
		cancel()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen for the doubles: %w", err)
	}
	s := &Server{gh: gh, claude: claude, dir: dir, ln: ln, ctx: ctx, cancel: cancel,
		pids: map[int]int{}, conns: map[net.Conn]struct{}{}}
	s.wg.Go(s.accept)
	return s, nil
}

// Serve starts a server for the test tb, as NewServer does, and registers
// its teardown: report every violation as a failure of tb, kill the doubles
// still running, and close the server.
func Serve(tb testing.TB, gh GH, claude *fakeclaude.Claude) *Server {
	tb.Helper()
	s, err := NewServer(gh, claude)
	if err != nil {
		tb.Fatalf("start the doubles' server: %v", err)
	}
	tb.Cleanup(func() {
		for _, v := range s.Violations() {
			tb.Errorf("acceptance: unknown call: %s", v)
		}
		s.KillDoubles()
		if err := s.Close(); err != nil {
			tb.Errorf("close the doubles' server: %v", err)
		}
	})
	return s
}

// link makes bin, holding gh and claude as symbolic links to the running
// test binary, which plays them when it runs under those names.
func link(bin string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find the test binary: %w", err)
	}
	if err := os.Mkdir(bin, binPerm); err != nil {
		return fmt.Errorf("make the doubles' bin directory: %w", err)
	}
	for _, name := range []string{ghName, claudeName} {
		if err := os.Symlink(exe, filepath.Join(bin, name)); err != nil {
			return fmt.Errorf("link the %s double: %w", name, err)
		}
	}
	return nil
}

// Socket returns the path of the server's socket, the value of SocketEnv
// for the doubles.
func (s *Server) Socket() string {
	return filepath.Join(s.dir, "s")
}

// Bin returns the directory that holds the gh and claude doubles, to put
// first on PATH.
func (s *Server) Bin() string {
	return filepath.Join(s.dir, "bin")
}

// Violations returns the calls the doubles could not answer, in the order
// they came: each the call as a quoted command line and why, in
// parentheses.
func (s *Server) Violations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.violations...)
}

// Err returns an error naming the first violation and how many there are,
// or nil when there is none.
func (s *Server) Err() error {
	v := s.Violations()
	switch len(v) {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("acceptance: unknown call: %s", v[0])
	}
	return fmt.Errorf("acceptance: unknown call: %s (and %d more)", v[0], len(v)-1)
}

// KillDoubles kills every double whose invocation is still in flight, such
// as a claude whose script still blocks after the program under test was
// stopped.
func (s *Server) KillDoubles() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for pid := range s.pids {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

// Close stops the server: it cancels the scripts still running, closes
// every connection, waits for the invocations to end and removes the
// server's directory.
func (s *Server) Close() error {
	s.cancel()
	err := s.ln.Close()
	s.mu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	if rmErr := os.RemoveAll(s.dir); rmErr != nil {
		return fmt.Errorf("remove the doubles' directory: %w", rmErr)
	}
	if err != nil {
		return fmt.Errorf("close the doubles' socket: %w", err)
	}
	return nil
}

// accept answers each connection, one invocation each, until Close.
func (s *Server) accept() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.ctx.Err() != nil { // Close already closed the connections
			s.mu.Unlock()
			_ = c.Close()
			return
		}
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		s.wg.Go(func() { s.serve(c) })
	}
}

// serve answers the invocation on c and closes it.
func (s *Server) serve(c net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
		_ = c.Close()
	}()
	var req request
	dec := json.NewDecoder(c)
	if err := dec.Decode(&req); err != nil {
		return
	}
	s.track(req.Pid, 1)
	defer s.track(req.Pid, -1)
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	// After its request the double sends only acknowledgements, one byte
	// each: the end of its input means it closed the connection, after a
	// SIGTERM or when it died.
	acks := make(chan struct{}, 1)
	s.wg.Go(func() {
		// What the decoder buffered is the rest of the request, such as its
		// newline, never an acknowledgement.
		_, _ = io.Copy(io.Discard, dec.Buffered())
		watch(c, acks)
		cancel()
	})
	out := &frames{enc: json.NewEncoder(c), ctx: ctx, acks: acks}
	code := s.answer(ctx, req, out)
	_ = out.send(frame{Exit: &code})
}

// track counts an invocation of the double pid in or out of flight. A pid
// that is not positive is never tracked: kill(2) reads 0 and -1 as process
// groups, which hold the test process itself.
func (s *Server) track(pid, delta int) {
	if pid <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pids[pid] += delta; s.pids[pid] <= 0 {
		delete(s.pids, pid)
	}
}

// answer runs req through the fake for its program, writes what it prints
// to out, journals a violation and returns the exit code.
func (s *Server) answer(ctx context.Context, req request, out *frames) int {
	var code int
	var violation string
	switch {
	case req.Name == ghName && s.gh != nil:
		r := s.gh.Run(fakegithub.Invocation{Args: req.Args, Stdin: req.Stdin, Env: req.Env})
		_ = out.send(frame{Stdout: r.Stdout, Stderr: r.Stderr})
		code, violation = r.Code, r.Violation
	case req.Name == claudeName && s.claude != nil:
		inv := fakeclaude.Invocation{
			Args: req.Args, Dir: req.Dir, Env: req.Env,
			IgnoreStop: out.ignoreTerm,
		}
		o := s.claude.Run(ctx, inv, out.writer(false), out.writer(true))
		code, violation = o.Code, o.Violation
	default:
		violation = fakegithub.CommandLine(req.Name, req.Args) + " (no fake answers " + req.Name + ")"
		_ = out.send(frame{Stderr: []byte("acceptance: unknown call: " + violation + "\n")})
		code = 1
	}
	if violation != "" {
		s.mu.Lock()
		s.violations = append(s.violations, violation)
		s.mu.Unlock()
	}
	return code
}

// watch reads the double's acknowledgements from r, passing each to acks,
// until r ends.
func watch(r io.Reader, acks chan<- struct{}) {
	b := make([]byte, 1)
	for {
		if _, err := r.Read(b); err != nil {
			return
		}
		select {
		case acks <- struct{}{}:
		default:
		}
	}
}

// frames sends frames on one connection, one at a time. ctx ends when the
// double closed the connection, and acks carries its acknowledgements.
type frames struct {
	mu   sync.Mutex
	enc  *json.Encoder
	ctx  context.Context //nolint:containedctx // the connection's lifetime, which ignoreTerm waits within
	acks <-chan struct{}
}

// ignoreTerm tells the double to ignore SIGTERM, and returns once it
// acknowledged that, or closed the connection.
func (f *frames) ignoreTerm() {
	if err := f.send(frame{IgnoreTerm: true}); err != nil {
		return
	}
	select {
	case <-f.acks:
	case <-f.ctx.Done():
	}
}

// send writes f as one line.
func (f *frames) send(fr frame) error {
	if len(fr.Stdout) == 0 && len(fr.Stderr) == 0 && !fr.IgnoreTerm && fr.Exit == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.enc.Encode(fr); err != nil {
		return fmt.Errorf("send a frame to the double: %w", err)
	}
	return nil
}

// writer returns a writer whose every write becomes a frame of standard
// output, or of standard error when stderr is true.
func (f *frames) writer(stderr bool) io.Writer {
	return writerFunc(func(p []byte) (int, error) {
		fr := frame{Stdout: p}
		if stderr {
			fr = frame{Stderr: p}
		}
		if err := f.send(fr); err != nil {
			return 0, err
		}
		return len(p), nil
	})
}

// writerFunc is a function that is an io.Writer.
type writerFunc func(p []byte) (int, error)

// Write implements io.Writer.
func (w writerFunc) Write(p []byte) (int, error) { return w(p) }
