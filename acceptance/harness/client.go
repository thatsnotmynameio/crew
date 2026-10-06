package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	// clientTimeout caps how long a double waits for its exit code.
	clientTimeout = 120 * time.Second
	// exitDoubleFailed is a double's exit code when it cannot reach the
	// test process or loses it before the exit code.
	exitDoubleFailed = 2
	// exitTerminated is a double's exit code after SIGTERM, as a shell
	// reports a process that SIGTERM ended.
	exitTerminated = 143
)

// client returns the double that plays the program name: a client of the
// Server whose socket SocketEnv names.
func client(name string) func() int {
	return func() int {
		return runClient(name, os.Stdin, os.Stdout, os.Stderr)
	}
}

// runClient sends the server this process's invocation of name, copies the
// frames it answers to stdout and stderr in the order they come, and
// returns the exit code of the last frame. On SIGTERM it closes the
// connection, which cancels the invocation in the server, and returns 143.
func runClient(name string, stdin *os.File, stdout, stderr io.Writer) int {
	terms := make(chan os.Signal, 1)
	signal.Notify(terms, syscall.SIGTERM)
	defer signal.Stop(terms)
	socket := os.Getenv(SocketEnv)
	if socket == "" {
		return doubleFailed(stderr, name, SocketEnv+" is not set; only the acceptance suite runs this double")
	}
	req, err := newRequest(name, stdin)
	if err != nil {
		return doubleFailed(stderr, name, err.Error())
	}
	c, err := (&net.Dialer{}).DialContext(context.Background(), "unix", socket)
	if err != nil {
		return doubleFailed(stderr, name, fmt.Sprintf("cannot reach the test process at %s: %v", socket, err))
	}
	defer c.Close()
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return doubleFailed(stderr, name, fmt.Sprintf("send the invocation to the test process: %v", err))
	}
	codes := make(chan int, 1)
	go func() { codes <- copyFrames(c, name, stdout, stderr) }()
	timeout := time.NewTimer(clientTimeout)
	defer timeout.Stop()
	select {
	case code := <-codes:
		return code
	case <-terms:
		_ = c.Close()
		return exitTerminated
	case <-timeout.C:
		return doubleFailed(stderr, name, fmt.Sprintf("no exit code from the test process within %v", clientTimeout))
	}
}

// copyFrames copies the frames on c to stdout and stderr until the exit
// frame, and returns its code.
func copyFrames(c io.Reader, name string, stdout, stderr io.Writer) int {
	dec := json.NewDecoder(c)
	for {
		var fr frame
		if err := dec.Decode(&fr); err != nil {
			return doubleFailed(stderr, name,
				fmt.Sprintf("the test process closed the connection before the exit code: %v", err))
		}
		_, _ = stdout.Write(fr.Stdout)
		_, _ = stderr.Write(fr.Stderr)
		if fr.Exit != nil {
			return *fr.Exit
		}
	}
}

// doubleFailed says on stderr why the double name could not run, and
// returns its exit code for that.
func doubleFailed(stderr io.Writer, name, why string) int {
	fmt.Fprintf(stderr, "acceptance: %s double: %s\n", name, why)
	return exitDoubleFailed
}

// newRequest returns this process's invocation of name: its arguments,
// working directory, environment variables GH_CONFIG_DIR and CREW_*, pid
// and, for gh, its standard input.
func newRequest(name string, stdin *os.File) (request, error) {
	dir, err := os.Getwd()
	if err != nil {
		return request{}, fmt.Errorf("find the working directory: %w", err)
	}
	req := request{Name: name, Args: os.Args[1:], Dir: dir, Env: map[string]string{}, Pid: os.Getpid()}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if k == "GH_CONFIG_DIR" || strings.HasPrefix(k, "CREW_") {
			req.Env[k] = v
		}
	}
	if name == ghName {
		if req.Stdin, err = readStdin(stdin); err != nil {
			return request{}, err
		}
	}
	return req, nil
}

// readStdin reads f, standard input, to its end, unless it is a terminal
// or another character device such as /dev/null, which a program that
// gets no input has as standard input.
func readStdin(f *os.File) ([]byte, error) {
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return nil, nil //nolint:nilerr // no readable standard input is no input
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read standard input: %w", err)
	}
	return b, nil
}
