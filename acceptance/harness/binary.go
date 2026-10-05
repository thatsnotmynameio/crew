package harness

import (
	"os"
	"testing"
)

// CrewBinEnv is the environment variable that holds the path of the crew
// binary under test. The local command sets it: it builds crew with the
// release config and runs the suite against that build.
const CrewBinEnv = "CREW_BIN"

// localCommand builds crew and runs the suite with CrewBinEnv set; it runs
// from the repository's root.
const localCommand = "go -C acceptance run ./cmd/acceptance"

// Binary returns the path of the crew binary under test, from CREW_BIN. When
// CREW_BIN is unset or empty, it fails the test, never skips it, and names
// the command that builds crew and runs the suite.
func Binary(tb testing.TB) string {
	tb.Helper()
	bin := os.Getenv(CrewBinEnv)
	if bin == "" {
		tb.Fatalf("%s is not set: run the acceptance suite with `%s` from the repository's root, "+
			"which builds crew and sets it", CrewBinEnv, localCommand)
	}
	return bin
}
