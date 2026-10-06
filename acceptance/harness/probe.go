package harness

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

const (
	// probeName is the name the test binary runs the probe under.
	probeName = "acceptance-probe"
	// probeStartEnv holds the seconds the probe's elapsed time starts at,
	// so a test can see it grow from 9s to 10s.
	probeStartEnv = "ACCEPTANCE_PROBE_START"
	// probeReplyTimeout is how long the probe waits for the reply to its
	// background-colour query before it gives up.
	probeReplyTimeout = 5 * time.Second
	// probeFrame is how often the probe redraws its header.
	probeFrame = 100 * time.Millisecond
	// probeSpinner are the frames of the probe's spinner, one per redraw.
	probeSpinner = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
	// probeMinRun and probeMinGap are the shortest fill run and the least
	// space before the header's last item, as in crew's header.
	probeMinRun = 3
	probeMinGap = 2
	// exitNoTerminal and exitNoReply are the probe's failures: its input is
	// not a terminal, or its background-colour query got no reply.
	exitNoTerminal = 2
	exitNoReply    = 3
	// keyBufferSize is the size of one read of the probe's input.
	keyBufferSize = 256
)

// probe is a full-screen program without Bubble Tea, to test the screen
// harness against. In raw mode, it asks the terminal for its background
// colour (OSC 11) and waits for the reply, then draws with cursor
// positioning: a header like crew's, with a fill run sized from the
// remaining width, the elapsed time, a clock and a spinner that change on
// every redraw, and a right-aligned item; the reply; a line at row 10,
// column 20; and the last row. It exits 0 on q or SIGINT, after writing
// "bye".
func probe() int {
	in, out := os.Stdin, os.Stdout
	state, err := term.MakeRaw(in.Fd())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: raw mode: %v\n", probeName, err)
		return exitNoTerminal
	}
	defer term.Restore(in.Fd(), state) //nolint:errcheck // the probe exits next; nothing can act on the error
	width, height, err := term.GetSize(out.Fd())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: terminal size: %v\n", probeName, err)
		return exitNoTerminal
	}
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	keys := readKeys(in)
	background, ok := queryBackground(out, keys)
	if !ok {
		fmt.Fprintf(os.Stderr, "%s: no reply to the OSC 11 background-colour query within %v\n",
			probeName, probeReplyTimeout)
		return exitNoReply
	}
	start, _ := strconv.Atoi(os.Getenv(probeStartEnv))
	view := probeView{out: out, width: width, start: start, began: time.Now()}
	fmt.Fprintf(out, "\x1b[2J\x1b[3;1Hbackground %s\x1b[10;20Hrow 10, column 20\x1b[%d;1Hrow %d",
		background, height, height)
	return view.run(keys, interrupts)
}

// readKeys reads the probe's input in a goroutine, until it fails.
func readKeys(in io.Reader) <-chan []byte {
	keys := make(chan []byte)
	go func() {
		defer close(keys)
		buf := make([]byte, keyBufferSize)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				keys <- bytes.Clone(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	return keys
}

// queryBackground asks the terminal for its background colour and returns
// the colour it replies, or false when no reply comes in time.
func queryBackground(out io.Writer, keys <-chan []byte) (string, bool) {
	fmt.Fprint(out, "\x1b]11;?\x07")
	timeout := time.After(probeReplyTimeout)
	var got []byte
	for {
		select {
		case b, open := <-keys:
			if !open {
				return "", false
			}
			got = append(got, b...)
			if colour, ok := oscReply(string(got)); ok {
				return colour, true
			}
		case <-timeout:
			return "", false
		}
	}
}

// oscReply returns the colour in an OSC 11 reply, ended by BEL or ST.
func oscReply(s string) (string, bool) {
	_, after, found := strings.Cut(s, "\x1b]11;")
	if !found {
		return "", false
	}
	if colour, _, ok := strings.Cut(after, "\x07"); ok {
		return colour, true
	}
	colour, _, ok := strings.Cut(after, "\x1b\\")
	return colour, ok
}

// probeView draws the probe's header.
type probeView struct {
	out   io.Writer
	width int
	start int
	began time.Time
}

// run redraws the header every frame until q or an interrupt.
func (v probeView) run(keys <-chan []byte, interrupts <-chan os.Signal) int {
	ticker := time.NewTicker(probeFrame)
	defer ticker.Stop()
	for frame := 0; ; frame++ {
		fmt.Fprintf(v.out, "\x1b[1;1H\x1b[2K%s", v.header(frame, time.Now()))
		select {
		case <-ticker.C:
		case b, open := <-keys:
			if !open || bytes.ContainsRune(b, 'q') {
				return v.bye()
			}
		case <-interrupts:
			return v.bye()
		}
	}
}

// bye writes the probe's last words, which a test reads after it exits.
func (v probeView) bye() int {
	fmt.Fprint(v.out, "\x1b[7;1Hbye")
	return 0
}

// header is the probe's first line, laid out as crew's: the name, a run of
// ╱ that fills the width the details leave, the details, and the last item
// at the right edge.
func (v probeView) header(frame int, now time.Time) string {
	spinner := []rune(probeSpinner)
	elapsed := v.start + int(now.Sub(v.began)/time.Second)
	details := fmt.Sprintf("up %ds • %s %c", elapsed, now.UTC().Format(time.TimeOnly), spinner[frame%len(spinner)])
	const name, right = "probe", "q quit"
	run := max(v.width-ansi.StringWidth(name+"  "+details)-probeMinGap-ansi.StringWidth(right), probeMinRun)
	line := name + " " + strings.Repeat("╱", run) + " " + details
	gap := max(v.width-ansi.StringWidth(line)-ansi.StringWidth(right), probeMinGap)
	return line + strings.Repeat(" ", gap) + right
}
