package fakegithub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// gh runs gh with args against g and returns its reply as is.
func gh(g *GitHub, args ...string) Reply {
	return g.Run(Invocation{Args: args})
}

// ok runs gh with args against g, fails the test unless it succeeded, and
// returns what it printed.
func ok(t *testing.T, g *GitHub, args ...string) string {
	t.Helper()
	r := gh(g, args...)
	if r.Code != 0 || r.Violation != "" {
		t.Fatalf("gh %q: code %d, violation %q, stderr %q", args, r.Code, r.Violation, r.Stderr)
	}
	return string(r.Stdout)
}

// refused runs gh with args against g, fails the test unless it failed
// without being a violation, and returns its stderr.
func refused(t *testing.T, g *GitHub, args ...string) string {
	t.Helper()
	r := gh(g, args...)
	if r.Code == 0 || r.Violation != "" {
		t.Fatalf("gh %q: code %d, violation %q, stdout %q; want a GitHub failure", args, r.Code, r.Violation, r.Stdout)
	}
	return string(r.Stderr)
}

// decode decodes out, gh's JSON output, into a value of type T.
func decode[T any](t *testing.T, out string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	return v
}

// at returns the time s, in RFC 3339, as GitHub writes it.
func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// wantLabels checks the labels of the issue or pull request number.
func wantLabels(t *testing.T, g *GitHub, number int, want ...string) {
	t.Helper()
	got := labelsOf(g, number)
	if !slices.Equal(got, want) {
		t.Errorf("labels of #%d = %q, want %q", number, got, want)
	}
}

// labelsOf returns the labels of the issue or pull request number.
func labelsOf(g *GitHub, number int) []string {
	if is, found := g.Issue(number); found {
		return is.Labels
	}
	pr, _ := g.PullRequest(number)
	return pr.Labels
}

// writeFile writes content to the file name in dir.
func writeFile(dir, name, content string) error {
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}
