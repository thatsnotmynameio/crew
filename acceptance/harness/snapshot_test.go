package harness

import (
	"errors"
	"flag"
	"io/fs"
	"os"
	"strings"
	"testing"
)

// accept sets -accept-snapshots to value for the test, and restores it.
func accept(t *testing.T, value string) {
	t.Helper()
	before := flag.Lookup(acceptFlag).Value.String()
	if err := flag.Set(acceptFlag, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flag.Set(acceptFlag, before) })
}

// Covers U5: a changed probe line fails with a diff of that line.
func TestAChangedLineFailsWithALineDiff(t *testing.T) {
	accept(t, "false")
	want, err := os.ReadFile("testdata/probe.snapshot")
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(strings.TrimSuffix(string(want), "\n"),
		"background rgb:0000/0000/0000", "background changed", 1)

	failed, msg := capture(func(tb testing.TB) { tb.Helper(); MatchSnapshot(tb, "probe", changed) })

	if diff := "line 3:\n-background rgb:0000/0000/0000\n+background changed\n"; !failed || !strings.HasSuffix(msg, diff) {
		t.Fatalf("failed = %v, message:\n%s\nwant it to end in:\n%s", failed, msg, diff)
	}
}

// Covers U5: a missing snapshot fails and names -accept-snapshots.
func TestAMissingSnapshotFailsAndNamesTheFlag(t *testing.T) {
	accept(t, "false")
	t.Chdir(t.TempDir())

	failed, msg := capture(func(tb testing.TB) { tb.Helper(); MatchSnapshot(tb, "probe", "a screen") })

	if !failed || !strings.Contains(msg, "-accept-snapshots") {
		t.Fatalf("failed = %v, message:\n%s", failed, msg)
	}
}

// Covers U5: with -accept-snapshots, the snapshot is written and the test
// passes; without it, the written snapshot then matches.
func TestAcceptSnapshotsWritesTheSnapshot(t *testing.T) {
	t.Chdir(t.TempDir())
	accept(t, "true")

	MatchSnapshot(t, "probe", "a screen\n\nlast row")

	got, err := os.ReadFile("testdata/probe.snapshot")
	if err != nil || string(got) != "a screen\n\nlast row\n" {
		t.Fatalf("snapshot = %q, %v", got, err)
	}
	accept(t, "false")
	MatchSnapshot(t, "probe", "a screen\n\nlast row")
}

// Covers U1 (KTD3): with -accept-snapshots, a test that already failed, such
// as on a content assertion, fails again instead of writing its snapshot.
func TestAcceptSnapshotsRefusesAfterAFailure(t *testing.T) {
	t.Chdir(t.TempDir())
	accept(t, "true")

	failed, msg := capture(func(tb testing.TB) {
		tb.Helper()
		tb.Errorf("the board shows no Handled card")
		MatchSnapshot(tb, "probe", "a screen")
	})

	if !failed || !strings.Contains(msg, "not written") {
		t.Fatalf("failed = %v, message:\n%s", failed, msg)
	}
	if _, err := os.Stat("testdata/probe.snapshot"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("snapshot written after a failure: stat error = %v", err)
	}
}
