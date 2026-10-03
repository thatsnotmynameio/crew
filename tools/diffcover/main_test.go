package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/thatsnotmynameio/crew"

// profile writes a coverage profile with the given blocks and returns its path.
func profile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "coverage.out")
	err := os.WriteFile(path, []byte("mode: atomic\n"+body), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	return path
}

// blocks writes one single-line block for each line from 1 to n of a.go,
// covered for lines up to covered.
func blocks(n, covered int) string {
	var b strings.Builder
	for line := 1; line <= n; line++ {
		count := 0
		if line <= covered {
			count = 1
		}
		b.WriteString(module + "/a.go:" + itoa(line) + ".1," + itoa(line) + ".10 1 " + itoa(count) + "\n")
	}

	return b.String()
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// addLines is a diff that adds lines 1 to n of a.go.
func addLines(n int) string {
	var b strings.Builder
	b.WriteString("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -0,0 +1," + itoa(n) + " @@\n")
	for range n {
		b.WriteString("+x\n")
	}

	return b.String()
}

func runCheck(t *testing.T, profileBody, diff string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run([]string{"-profile", profile(t, profileBody)}, strings.NewReader(diff), &stdout, &stderr)

	return code, stdout.String() + stderr.String()
}

func TestBelowThresholdFails(t *testing.T) {
	code, out := runCheck(t, blocks(20, 17), addLines(20))
	if code != 1 || !strings.Contains(out, "17 of 20") || !strings.Contains(out, "85.0%") {
		t.Fatalf("code %d, output %q; want 1 and 17 of 20 (85.0%%)", code, out)
	}
}

func TestAtThresholdPasses(t *testing.T) {
	code, out := runCheck(t, blocks(20, 18), addLines(20))
	if code != 0 || !strings.Contains(out, "90.0%") {
		t.Fatalf("code %d, output %q; want 0 and 90.0%%", code, out)
	}
}

func TestNoGoChangePasses(t *testing.T) {
	diff := "diff --git a/docs/a.mdx b/docs/a.mdx\n--- a/docs/a.mdx\n+++ b/docs/a.mdx\n@@ -1,0 +2,1 @@\n+text\n" +
		"diff --git a/.github/x.yml b/.github/x.yml\n--- a/.github/x.yml\n+++ b/.github/x.yml\n@@ -1,0 +2,1 @@\n+on: push\n"
	code, out := runCheck(t, blocks(5, 0), diff)
	if code != 0 || !strings.Contains(out, "no coverable lines changed") {
		t.Fatalf("code %d, output %q; want 0 and no coverable lines", code, out)
	}
}

func TestOnlyDeletedLinesPass(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -3,2 +2,0 @@\n-x\n-y\n"
	code, out := runCheck(t, blocks(5, 0), diff)
	if code != 0 || !strings.Contains(out, "no coverable lines changed") {
		t.Fatalf("code %d, output %q; want 0 and no coverable lines", code, out)
	}
}

func TestRepeatedBlockCountsAsCoveredWhenAnyCopyRan(t *testing.T) {
	body := module + "/a.go:1.1,1.10 1 0\n" + module + "/a.go:1.1,1.10 1 3\n"
	code, out := runCheck(t, body, addLines(1))
	if code != 0 || !strings.Contains(out, "1 of 1") {
		t.Fatalf("code %d, output %q; want 0 and 1 of 1", code, out)
	}
}

func TestLineOutsideAnyBlockIsNotCoverable(t *testing.T) {
	// Block covers line 2 only; lines 1 and 3 are a comment and a blank line.
	body := module + "/a.go:2.1,2.10 1 1\n"
	code, out := runCheck(t, body, addLines(3))
	if code != 0 || !strings.Contains(out, "1 of 1") {
		t.Fatalf("code %d, output %q; want 0 and 1 of 1", code, out)
	}
}

func TestAddedLineStartingWithPlusIsNotAHeader(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -0,0 +1,2 @@\n+++ not a header\n+x\n"
	code, out := runCheck(t, blocks(2, 2), diff)
	if code != 0 || !strings.Contains(out, "2 of 2") {
		t.Fatalf("code %d, output %q; want 0 and 2 of 2", code, out)
	}
}

func TestMalformedProfileNamesTheFile(t *testing.T) {
	path := profile(t, "not a block\n")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-profile", path}, strings.NewReader(addLines(1)), &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), path) {
		t.Fatalf("code %d, stderr %q; want 2 naming %s", code, stderr.String(), path)
	}
}
