package fakegithub_test

import (
	"testing"

	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

// TestMain goes through harness.Main, as every package of the suite does,
// so the suite's flags such as -accept-snapshots reach every test binary.
func TestMain(m *testing.M) {
	harness.Main(m)
}
