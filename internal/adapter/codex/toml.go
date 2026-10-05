package codex

import (
	"fmt"
	"strings"
)

// tomlString returns s as a TOML basic string, the form a `codex -c` value
// must take to be read as the text it is: Codex parses each value as TOML,
// and silently keeps the raw text, outer quotes stripped, when that fails.
// A quote and a backslash are escaped, and a control character is written
// as \uXXXX; Go's %q would write \x.. and \a, which TOML rejects.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
