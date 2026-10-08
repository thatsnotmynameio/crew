package main

import "strings"

// sectionPrefix starts every version's heading in CHANGELOG.md, and ends
// the section before it.
const sectionPrefix = "## "

// section returns the text of version's section in changelog, trimmed and
// without its heading: the lines after the one that reads exactly
// "## <version>", up to the next line that starts with "## ". A byte-order
// mark, carriage returns and trailing whitespace on a heading are ignored.
// A missing, empty or repeated section is a refusal.
func section(changelog, version string) (string, error) {
	changelog = strings.ReplaceAll(strings.TrimPrefix(changelog, "\ufeff"), "\r", "")
	heading := sectionPrefix + version
	var text []string
	found, inside := false, false
	for line := range strings.SplitSeq(changelog, "\n") {
		if strings.HasPrefix(line, sectionPrefix) {
			inside = strings.TrimRight(line, " \t") == heading
			if inside && found {
				return "", refusalf("%s has two changelog sections: %q appears twice in CHANGELOG.md", version, heading)
			}
			found = found || inside

			continue
		}
		if inside {
			text = append(text, line)
		}
	}
	notes := strings.TrimSpace(strings.Join(text, "\n"))
	if !found {
		return "", refusalf("%s has no changelog section: no line of CHANGELOG.md reads %q", version, heading)
	}
	if notes == "" {
		return "", refusalf("%s has no changelog section: CHANGELOG.md has nothing under %q", version, heading)
	}

	return notes, nil
}
