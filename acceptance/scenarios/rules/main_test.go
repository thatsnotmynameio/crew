// Package rules holds the tester's scenarios for crew's rules: the labels an
// issue moves through, from a rule's ready label to its success or failure
// label, and the sessions and checks that decide which.
package rules

import (
	"testing"

	"github.com/thatsnotmynameio/crew/acceptance/harness"
)

func TestMain(m *testing.M) {
	harness.Main(m)
}
