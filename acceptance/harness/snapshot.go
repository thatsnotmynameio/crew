package harness

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// snapshotDirPerm and snapshotPerm are the modes of a written snapshot
	// and its testdata directory.
	snapshotDirPerm = 0o750
	snapshotPerm    = 0o600
)

// MatchSnapshot compares text, a masked screen, with the snapshot name in
// the testdata directory of the test's package. A missing snapshot fails
// and names -accept-snapshots; with that flag, MatchSnapshot writes the
// snapshot instead and passes. A mismatch fails with the lines that differ.
func MatchSnapshot(tb testing.TB, name, text string) {
	tb.Helper()
	path := filepath.Join("testdata", name+".snapshot")
	if accepting() {
		if err := os.MkdirAll(filepath.Dir(path), snapshotDirPerm); err != nil {
			tb.Fatalf("write snapshot %s: %v", path, err)
			return
		}
		//nolint:gosec // G703: the path is the test's own snapshot in its testdata, named by the test
		if err := os.WriteFile(path, []byte(text+"\n"), snapshotPerm); err != nil {
			tb.Fatalf("write snapshot %s: %v", path, err)
		}
		return
	}
	//nolint:gosec // G304: the path is the test's own snapshot in its testdata, named by the test
	want, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		tb.Fatalf("snapshot %s is missing: run the test with -%s to write it", path, acceptFlag)
		return
	}
	if err != nil {
		tb.Fatalf("read snapshot %s: %v", path, err)
		return
	}
	if diff := lineDiff(strings.TrimSuffix(string(want), "\n"), text); diff != "" {
		tb.Errorf("the screen does not match snapshot %s (-want +got):\n%s", path, diff)
	}
}

// accepting reports whether the test binary runs with -accept-snapshots,
// which Main registers.
func accepting() bool {
	f := flag.Lookup(acceptFlag)
	return f != nil && f.Value.String() == "true"
}

// lineDiff lists the lines where got differs from want, by line number, or
// is empty when they are equal. A screen has one line per row, so lines
// compare by position.
func lineDiff(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := range max(len(wantLines), len(gotLines)) {
		w, wok := line(wantLines, i)
		g, gok := line(gotLines, i)
		if w == g && wok == gok {
			continue
		}
		fmt.Fprintf(&b, "line %d:\n", i+1)
		if wok {
			fmt.Fprintf(&b, "-%s\n", w)
		}
		if gok {
			fmt.Fprintf(&b, "+%s\n", g)
		}
	}
	return b.String()
}

// line is lines[i], and whether there is one.
func line(lines []string, i int) (string, bool) {
	if i < len(lines) {
		return lines[i], true
	}
	return "", false
}
