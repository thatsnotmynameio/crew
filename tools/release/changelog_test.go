package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSectionIsTheTextUnderItsHeading(t *testing.T) {
	changelog := "# Changelog\n\nNewest first.\n\n## 0.2.1\n\nFixes the thing.\n\n### Fixes\n\n- One.\n\n" +
		"## 0.2.0\n\nOlder.\n"
	got, err := section(changelog, "0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Fixes the thing.\n\n### Fixes\n\n- One."; got != want {
		t.Fatalf("section = %q, want %q", got, want)
	}
}

func TestSectionRunsToTheEndOfTheFile(t *testing.T) {
	got, err := section("# Changelog\n\n## 0.2.1\n\nLast one.\n\n", "0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Last one." {
		t.Fatalf("section = %q, want %q", got, "Last one.")
	}
}

func TestSectionReadsAWindowsFile(t *testing.T) {
	changelog := "\ufeff# Changelog\r\n\r\n## 0.2.1  \r\n\r\nSaved on Windows.\r\n\r\n## 0.2.0\r\n\r\nOlder.\r\n"
	got, err := section(changelog, "0.2.1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Saved on Windows." {
		t.Fatalf("section = %q, want %q", got, "Saved on Windows.")
	}
}

// missingSections are changelogs with no section for 0.2.1.
var missingSections = map[string]string{
	"only an older version": "# Changelog\n\n## 0.2.0\n\nOlder.\n",
	"a longer version":      "# Changelog\n\n## 0.2.10\n\nLonger.\n",
	"a prerelease":          "# Changelog\n\n## 0.2.1-rc1\n\nCandidate.\n",
	"a v prefix":            "# Changelog\n\n## v0.2.1\n\nPrefixed.\n",
	"brackets and a date":   "# Changelog\n\n## [0.2.1] - 2026-10-08\n\nKeep a changelog.\n",
	"a deeper heading":      "# Changelog\n\n### 0.2.1\n\nToo deep.\n",
	"an empty file":         "",
}

func TestSectionRefusesAMissingSection(t *testing.T) {
	for name, changelog := range missingSections {
		t.Run(name, func(t *testing.T) {
			_, err := section(changelog, "0.2.1")
			if _, ok := errors.AsType[refusal](err); !ok || !strings.Contains(err.Error(), "0.2.1 has no changelog section") ||
				!strings.Contains(err.Error(), `"## 0.2.1"`) {
				t.Fatalf("err = %v, want a refusal saying 0.2.1 has no changelog section and naming its heading", err)
			}
		})
	}
}

func TestSectionRefusesAnEmptySection(t *testing.T) {
	_, err := section("# Changelog\n\n## 0.2.1\n\n   \n\n## 0.2.0\n\nOlder.\n", "0.2.1")
	if _, ok := errors.AsType[refusal](err); !ok || !strings.Contains(err.Error(), "0.2.1 has no changelog section") {
		t.Fatalf("err = %v, want a refusal saying 0.2.1 has no changelog section", err)
	}
}

func TestSectionRefusesADuplicateSection(t *testing.T) {
	_, err := section("# Changelog\n\n## 0.2.1\n\nOne.\n\n## 0.2.1\n\nTwo.\n", "0.2.1")
	if _, ok := errors.AsType[refusal](err); !ok || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("err = %v, want a refusal naming the duplicate", err)
	}
}

// The repository's own CHANGELOG.md keeps the format the release reads.
func TestTheRepositoryChangelogHasTheFirstVersion(t *testing.T) {
	data, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := section(string(data), "0.1.0"); err != nil {
		t.Fatalf("CHANGELOG.md: %v", err)
	}
}
