package codex

import (
	"strconv"
	"testing"
)

func TestTOMLStringEscapesWhatTOMLRequires(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", `""`},
		{"alice bob", `"alice bob"`},
		{`a"b\c`, `"a\"b\\c"`},
		{"tab\there", `"tab\u0009here"`},
		{"two\nlines\r", `"two\u000Alines\u000D"`},
		{"nul\x00esc\x1bdel\x7f", `"nul\u0000esc\u001Bdel\u007F"`},
		{"crew-ödön[bot] ✓", `"crew-ödön[bot] ✓"`},
	}
	for _, tt := range tests {
		if got := tomlString(tt.in); got != tt.want {
			t.Errorf("tomlString(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

// The values crew's bots set decode back to themselves. The escapes the
// encoder writes mean the same in TOML and in Go, so strconv.Unquote reads
// them as TOML does.
func TestTOMLStringRoundTripsTheBotsValues(t *testing.T) {
	for _, value := range []string{
		"!GH_CONFIG_DIR='/home/me/.config/gh' gh auth git-credential",
		"git interpret-trailers --in-place --if-exists addIfDifferent --trailer " +
			"'Co-authored-by: crew-x[bot] <1+crew-x[bot]@users.noreply.github.com>'",
		"",
		`C:\path with "quotes"` + "\tand\x00control",
	} {
		got, err := strconv.Unquote(tomlString(value))
		if err != nil || got != value {
			t.Errorf("Unquote(tomlString(%q)) = %q, %v; want the value back", value, got, err)
		}
	}
}
