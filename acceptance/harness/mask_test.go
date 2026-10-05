package harness

import "testing"

// Covers U5: a mask turns 9s and 10s into the same placeholder, and the
// rest of the line does not shift.
func TestAMaskTurnsNineAndTenSecondsIntoTheSamePlaceholder(t *testing.T) {
	nine := MaskText("crew • up 9s • 2 running", DefaultMasks()...)
	ten := MaskText("crew • up 10s • 2 running", DefaultMasks()...)

	if want := "crew • up ## • 2 running"; nine != want || ten != want {
		t.Fatalf("masked = %q and %q, want %q", nine, ten, want)
	}
}

func TestTheDefaultMasks(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"clock", "14:30:05 started", "HH:MM:SS started"},
		{"cost", "$0.42, 12K tokens", "$#.##, 12K tokens"},
		{"minutes and seconds", "took 1m5s", "took ##"},
		{"hours and minutes", "up 2h03m", "up ##"},
		{"time left", "4m left", "## left"},
		{"spinner", "⠙ implement #9", "⠿ implement #9"},
		{"fill run", "crew ╱╱╱╱╱╱╱ crew/repo", "crew ╱╱ crew/repo"},
		{"no duration inside a word", "issue #9 x9s v0.1.0", "issue #9 x9s v0.1.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MaskText(tc.in, DefaultMasks()...); got != tc.want {
				t.Fatalf("MaskText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
