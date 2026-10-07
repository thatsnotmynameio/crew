package crew

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// SessionText is one line on how an action's session ended, such as the
// reason of its Outcome: the session's or the harness's words, a check's
// reason, or crew's own. The engine builds it from text that comes from
// outside crew, and NewSessionText strips it (StripControls). The failure
// report and RuleEnd never carry it.
type SessionText struct {
	text string
}

// NewSessionText returns s, stripped by StripControls, as a SessionText.
func NewSessionText(s string) SessionText {
	return SessionText{text: StripControls(s)}
}

// String returns the text, without control characters.
func (t SessionText) String() string {
	return t.text
}

// Said is what a running session last said, as its harness reports it, for
// the live view and the issue's status. The engine builds it from the
// harness's words, and NewSaid strips them (StripControls).
type Said struct {
	text string
}

// NewSaid returns s, stripped by StripControls, as a Said.
func NewSaid(s string) Said {
	return Said{text: StripControls(s)}
}

// String returns the text, without control characters.
func (t Said) String() string {
	return t.text
}

// CheckReason is one line on how a check ended: crew's words naming the
// check, followed by the last line the check printed. The engine builds it
// from the check's output, and NewCheckReason strips it (StripControls).
type CheckReason struct {
	text string
}

// NewCheckReason returns s, stripped by StripControls, as a CheckReason.
func NewCheckReason(s string) CheckReason {
	return CheckReason{text: StripControls(s)}
}

// String returns the text, without control characters.
func (t CheckReason) String() string {
	return t.text
}

// StripControls returns s without its escape sequences, invalid UTF-8 or
// control characters, so that it cannot reach a terminal or a comment as a
// control sequence. It removes each escape sequence whole, as a terminal
// parser reads it (the one ansi.Strip follows): CSI, OSC, DCS, SOS, PM and APC
// strings up to their terminator or the end of s, two-byte ESC sequences with
// their intermediates such as ESC ( B, and the 8-bit CSI byte. It drops
// invalid UTF-8, keeps tabs, and turns every other control character (C0, DEL
// and C1) into one space. It neither trims nor folds spaces, so text without
// controls comes out unchanged, and stripping twice changes nothing more.
func StripControls(s string) string {
	return strip(s, shown)
}

// StripControlsKeepingLines returns s stripped as StripControls strips it,
// but keeping its lines, for a body of several lines such as a Markdown
// comment: it keeps each line break (\n), drops each carriage return, so
// that \r\n becomes \n, and turns every other control character into one
// space, as StripControls does. Stripping twice changes nothing more.
func StripControlsKeepingLines(s string) string {
	return strip(s, shownKeepingLines)
}

// strip returns s without its escape sequences and invalid UTF-8, writing
// each rune of its text through show.
func strip(s string, show func(*strings.Builder, rune)) string {
	var b strings.Builder
	b.Grow(len(s))
	state := inText
	for i := 0; i < len(s); {
		if c := s[i]; c >= utf8.RuneSelf && state.readsRunes() {
			if r, size := utf8.DecodeRuneInString(s[i:]); size > 1 {
				// A terminal shows a rune wherever it is outside an OSC or
				// DCS string, and goes back to text.
				show(&b, r)
				state = inText
				i += size
				continue
			}
		}
		next, shows := state.next(s[i])
		if shows {
			show(&b, rune(s[i]))
		}
		state = next
		i++
	}
	return b.String()
}

// shown writes r to b as StripControls shows it: a tab or a printable rune
// as is, any other control character as a space.
func shown(b *strings.Builder, r rune) {
	if r != '\t' && unicode.IsControl(r) {
		r = ' '
	}
	b.WriteRune(r)
}

// shownKeepingLines writes r to b as StripControlsKeepingLines shows it: a
// line break as is, a carriage return not at all, anything else as shown
// writes it.
func shownKeepingLines(b *strings.Builder, r rune) {
	switch r {
	case '\n':
		b.WriteRune(r)
	case '\r':
	default:
		shown(b, r)
	}
}

// The control bytes the escape sequence parser of StripControls tells apart.
const (
	bel  = 0x07 // ends an OSC string
	can  = 0x18 // cancels a sequence
	sub  = 0x1a // cancels a sequence
	esc  = 0x1b // starts a sequence
	del  = 0x7f // ignored inside a sequence
	c1   = 0x80 // the first C1 control byte
	dcs8 = 0x90 // the 8-bit DCS
	sos8 = 0x98 // the 8-bit SOS
	csi8 = 0x9b // the 8-bit CSI
	st8  = 0x9c // the 8-bit string terminator
	osc8 = 0x9d // the 8-bit OSC
	pm8  = 0x9e // the 8-bit PM
	apc8 = 0x9f // the 8-bit APC
)

// stripState is where the escape sequence parser of StripControls stands,
// after the states of the DEC ANSI parser that ansi.Strip follows.
type stripState uint8

const (
	inText            stripState = iota // text, shown
	inEscape                            // after ESC
	inIntermediate                      // in an ESC or CSI sequence's intermediates
	inCSI                               // in a CSI sequence's parameters
	inDCSEntry                          // after a DCS introducer
	inDCSParam                          // in a DCS sequence's parameters
	inDCSIntermediate                   // in a DCS sequence's intermediates
	inDCSString                         // in a DCS sequence's data
	inOSCString                         // in an OSC string
	inSOSString                         // in an SOS, PM or APC string
)

// readsRunes reports whether a UTF-8 rune in state s is text: everywhere but
// in an OSC or DCS string, which take any byte as their own.
func (s stripState) readsRunes() bool {
	return s != inOSCString && s != inDCSString
}

// next returns the state after byte c in state s, and whether c is text to
// show.
func (s stripState) next(c byte) (stripState, bool) {
	switch {
	case s == inDCSString || s == inOSCString || s == inSOSString:
		return s.nextInString(c), false
	case s == inDCSEntry && (c == esc || c >= '\b' && c <= '\r'):
		return inDCSString, false
	}
	if next, ok := fromAnywhere(c); ok {
		return next, c == can || c == sub
	}
	switch {
	case s == inText:
		return inText, true
	case c < ' ' || c == del:
		// A control byte inside a sequence: a terminal still runs it,
		// except DEL, and a DCS's head ignores it.
		return s, c != del && s < inDCSEntry
	}
	return s.nextInHead(c), false
}

// nextInHead returns the state after the printable byte c in the head of a
// sequence: after ESC, or in the parameters or intermediates of a CSI or a
// DCS.
func (s stripState) nextInHead(c byte) stripState {
	switch s {
	case inEscape:
		return escapeNext(c)
	case inCSI:
		return paramNext(c, inIntermediate, inCSI, inText)
	case inIntermediate:
		return paramNext(c, inIntermediate, inText, inText)
	case inDCSIntermediate:
		return paramNext(c, inDCSIntermediate, inDCSString, inDCSString)
	default:
		// The head of a DCS: its entry or its parameters.
		return paramNext(c, inDCSIntermediate, inDCSParam, inDCSString)
	}
}

// nextInString returns the state after byte c in the string state s.
func (s stripState) nextInString(c byte) stripState {
	switch {
	case c == esc:
		return inEscape
	case c == can || c == sub || c == st8 || c == bel && s == inOSCString:
		return inText
	case s == inSOSString && c >= c1:
		next, _ := fromAnywhere(c)
		return next
	}
	return s
}

// fromAnywhere returns the state that byte c starts in any state but a
// string's, and false when c does not start one: CAN and SUB end a sequence,
// ESC and the 8-bit introducers start one, and any other byte from 0x80 up
// that is not part of a rune is dropped.
func fromAnywhere(c byte) (stripState, bool) {
	switch {
	case c == esc:
		return inEscape, true
	case c == csi8:
		return inCSI, true
	case c == dcs8:
		return inDCSEntry, true
	case c == osc8:
		return inOSCString, true
	case c == sos8 || c == pm8 || c == apc8:
		return inSOSString, true
	case c >= c1 || c == can || c == sub:
		return inText, true
	}
	return inText, false
}

// escapeNext returns the state after the printable byte c that follows ESC.
func escapeNext(c byte) stripState {
	switch c {
	case '[':
		return inCSI
	case ']':
		return inOSCString
	case 'P':
		return inDCSEntry
	case 'X', '^', '_':
		return inSOSString
	}
	if c < '0' {
		return inIntermediate
	}
	return inText
}

// paramNext returns the state after the printable byte c in a sequence's
// head: intermediate after an intermediate byte (0x20 to 0x2F), param after a
// parameter byte (0x30 to 0x3F), and final after any other.
func paramNext(c byte, intermediate, param, final stripState) stripState {
	switch {
	case c < '0':
		return intermediate
	case c < '@':
		return param
	}
	return final
}
