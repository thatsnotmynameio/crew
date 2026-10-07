package crew

import (
	"slices"
	"strings"
	"testing"
)

// strippedText is each input of the stripping rule with what it gives.
var strippedText = []struct {
	name, in, want string
}{
	{"plain text", "the check tests passed: ok", "the check tests passed: ok"},
	{"spaces are neither trimmed nor folded", "  two  spaces  ", "  two  spaces  "},
	{"wide and accented runes", "café ✓ 日本", "café ✓ 日本"},
	{"a NUL", "a\x00b", "a b"},
	{"DEL", "a\x7fb", "a b"},
	{"a C1 character", "a\u0085b", "a b"},
	{"line breaks", "fatal: x\nhint: y", "fatal: x hint: y"},
	{"a carriage return", "a\rb", "a b"},
	{"a vertical tab", "a\vb", "a b"},
	{"a form feed", "a\fb", "a b"},
	{"a bell", "ring\x07 the bell", "ring  the bell"},
	{"a tab", "a\tb", "a\tb"},
	{"colours", "\x1b[31mred\x1b[0m", "red"},
	{"a true colour", "\x1b[38;2;1;2;3mrgb", "rgb"},
	{"a title ended by BEL", "a\x1b]0;evil\x07b", "ab"},
	{"a title ended by ESC \\", "a\x1b]0;evil\x1b\\b", "ab"},
	{"an unterminated title", "a\x1b]0;evil and the rest", "a"},
	{"a hyperlink", "\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\", "link"},
	{"a charset and a reset", "\x1b(Bdone\x1b[m", "done"},
	{"a cursor save", "a\x1b7b", "ab"},
	{"a DCS string", "a\x1bPq#0;2;0;0;0\x1b\\b", "ab"},
	{"an APC string", "a\x1b_Gf=24;payload\x1b\\b", "ab"},
	{"a PM string", "a\x1b^private\x1b\\b", "ab"},
	{"an SOS string", "a\x1bXstart\x1b\\b", "ab"},
	{"a lone ESC at the end", "a\x1b", "a"},
	{"the 8-bit CSI byte with its parameters", "a\x9b31mb", "ab"},
	{"invalid UTF-8", "a\xffb\xc3", "ab"},
	{"a control inside a CSI", "a\x1b[3\n1mb", "a b"},
	{"a CSI cancelled by CAN", "a\x1b[3\x18b", "a b"},
	{"a DEL inside a CSI", "a\x1b[3\x7f1mb", "ab"},
	{"a CSI's intermediates end at a parameter byte", "a\x1b[ 1mb", "amb"},
	{"a DCS with intermediates", "a\x1bP $qm\x1b\\b", "ab"},
	{"a DCS whose data starts with a line break", "a\x1bP\nq\x1b\\b", "ab"},
	{"a string cancelled by SUB", "a\x1b]0;x\x1ab", "ab"},
	{"a rune inside an APC string", "a\x1b_é\x1b\\b", "aéb"},
	{"a C1 byte inside an SOS string", "a\x1bXs\x85b", "ab"},
	{"the 8-bit introducers and terminator", "a\x90q\x9c\x98s\x9c\x9d0;t\x07\x9e\x9cb", "ab"},
}

// builders builds each text type from a string, returning its String.
var builders = map[string]func(string) string{
	"SessionText": func(s string) string { return NewSessionText(s).String() },
	"Said":        func(s string) string { return NewSaid(s).String() },
	"CheckReason": func(s string) string { return NewCheckReason(s).String() },
}

func TestStripControls(t *testing.T) {
	for _, tt := range strippedText {
		if got := StripControls(tt.in); got != tt.want {
			t.Errorf("%s: StripControls(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestEachTextTypeStripsWhatItIsBuiltFrom(t *testing.T) {
	for typ, build := range builders {
		for _, tt := range strippedText {
			if got := build(tt.in); got != tt.want {
				t.Errorf("%s: New%s(%q) = %q, want %q", tt.name, typ, tt.in, got, tt.want)
			}
		}
	}
}

func TestStripControlsIsIdempotent(t *testing.T) {
	for _, tt := range strippedText {
		once := NewSessionText(tt.in)
		if twice := NewSessionText(once.String()); twice != once {
			t.Errorf("%s: SessionText rebuilt from %q is %q", tt.name, once, twice)
		}
		said := NewSaid(tt.in)
		if twice := NewSaid(said.String()); twice != said {
			t.Errorf("%s: Said rebuilt from %q is %q", tt.name, said, twice)
		}
		reason := NewCheckReason(tt.in)
		if twice := NewCheckReason(reason.String()); twice != reason {
			t.Errorf("%s: CheckReason rebuilt from %q is %q", tt.name, reason, twice)
		}
	}
}

func TestTheZeroValueIsTheEmptyText(t *testing.T) {
	if s := (SessionText{}); s.String() != "" || s != NewSessionText("") {
		t.Errorf("zero SessionText = %q, want the empty text", s)
	}
	if s := (Said{}); s.String() != "" || s != NewSaid("") {
		t.Errorf("zero Said = %q, want the empty text", s)
	}
	if s := (CheckReason{}); s.String() != "" || s != NewCheckReason("") {
		t.Errorf("zero CheckReason = %q, want the empty text", s)
	}
}

// linesKept is each input whose lines StripControlsKeepingLines keeps with
// what it gives; it strips every other input as StripControls does.
var linesKept = []struct {
	name, in, want string
}{
	{"line breaks", "fatal: x\nhint: y", "fatal: x\nhint: y"},
	{"a Markdown body", "## Title\n\n- one\n- two\n", "## Title\n\n- one\n- two\n"},
	{"a CRLF line break", "a\r\nb\r\n", "a\nb\n"},
	{"a bare carriage return", "a\rb", "ab"},
	{"a tab", "a\tb\n\tc", "a\tb\n\tc"},
	{"a line break inside a CSI", "a\x1b[3\n1mb", "a\nb"},
	{"a carriage return inside a CSI", "a\x1b[3\r1mb", "ab"},
	{"a NUL, a CR, a CRLF and colours", "a\x00b\rc\r\n\x1b[31md\x1b[0m", "a bc\nd"},
}

func TestStripControlsKeepingLines(t *testing.T) {
	for _, tt := range linesKept {
		if got := StripControlsKeepingLines(tt.in); got != tt.want {
			t.Errorf("%s: StripControlsKeepingLines(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
	for _, tt := range strippedText {
		if strings.ContainsAny(tt.in, "\n\r") {
			continue
		}
		if got := StripControlsKeepingLines(tt.in); got != tt.want {
			t.Errorf("%s: StripControlsKeepingLines(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestStripControlsKeepingLinesIsIdempotent(t *testing.T) {
	for _, tt := range slices.Concat(linesKept, strippedText) {
		once := StripControlsKeepingLines(tt.in)
		if twice := StripControlsKeepingLines(once); twice != once {
			t.Errorf("%s: stripping %q again gives %q", tt.name, once, twice)
		}
	}
}
