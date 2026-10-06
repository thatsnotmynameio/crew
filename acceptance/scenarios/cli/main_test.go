// Package cli holds the tester's scenarios for crew's command line: its flags,
// its arguments and the exit codes they end with.
package cli

import (
	"testing"

	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

func TestMain(m *testing.M) {
	harness.Main(m)
}
