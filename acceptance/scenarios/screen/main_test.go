// Package screen holds the tester's scenarios for crew's live view: what its
// screen shows in a terminal, with content assertions and snapshots.
package screen

import (
	"testing"

	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

func TestMain(m *testing.M) {
	harness.Main(m)
}
