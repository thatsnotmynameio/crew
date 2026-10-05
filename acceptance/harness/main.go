// Package harness runs programs for the acceptance suite: a screen harness
// that runs a program in a pseudo-terminal and reads its screen through a
// terminal emulator, masks for the parts of a screen that change on every
// run, snapshots of masked screens, and the doubles that the test binary
// plays when it runs under another name.
package harness

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// acceptFlag is the flag that rewrites the snapshots in testdata. It is not
// -update, so rewriting crew's own golden files never touches the suite's
// snapshots (KTD9).
const acceptFlag = "accept-snapshots"

// Main is every test package's TestMain. When the test binary runs under
// the name of a double (gh, claude or the probe), Main runs that double and
// exits with its code; otherwise it registers -accept-snapshots and runs
// the tests.
func Main(m *testing.M) {
	if double, ok := doubles()[filepath.Base(os.Args[0])]; ok {
		os.Exit(double())
	}
	flag.Bool(acceptFlag, false, "rewrite the screen snapshots in testdata")
	os.Exit(m.Run())
}

// doubles are the programs the test binary plays, by the name it runs
// under: a test starts the binary with that name as argv[0], through a
// symbolic link on PATH or exec.Cmd's Args.
func doubles() map[string]func() int {
	return map[string]func() int{
		probeName:  probe,
		ghName:     client(ghName),
		claudeName: client(claudeName),
	}
}
