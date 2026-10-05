package harness

import "regexp"

// Mask hides a part of a screen that changes on every run. Each whole match
// of Pattern becomes Placeholder, so the masked text always has the
// placeholder's width there: a wider match is clipped to it and a narrower
// one padded to it. A placeholder no wider than the narrowest real match
// keeps a line's columns in the common case, and clipping keeps them equal
// across runs where the value grows, as 9s becoming 10s (KTD9).
type Mask struct {
	Pattern     *regexp.Regexp
	Placeholder string
}

// DefaultMasks are the masks for what a screen derives from the wall clock,
// in the order they apply:
//   - clocks, such as 14:30:05;
//   - costs, such as $0.42;
//   - durations, such as 9s, 1m5s, 2h03m, which cover "up 9s" and
//     "4m left";
//   - spinner frames, the braille dots of Bubbles' MiniDot;
//   - fill runs of ╱, whose width a layout such as crew's header sizes
//     from what the rest of the line leaves: a run becomes a placeholder
//     shorter than any real run, so it always clips to the same width.
func DefaultMasks() []Mask {
	return []Mask{
		{Pattern: regexp.MustCompile(`\b\d{2}:\d{2}:\d{2}\b`), Placeholder: "HH:MM:SS"},
		{Pattern: regexp.MustCompile(`\$\d+\.\d{2}\b`), Placeholder: "$#.##"},
		{Pattern: regexp.MustCompile(`\b(?:\d+h(?:\d+m)?(?:\d+s)?|\d+m(?:\d+s)?|\d+s)\b`), Placeholder: "##"},
		{Pattern: regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏]`), Placeholder: "⠿"},
		{Pattern: regexp.MustCompile(`╱{2,}`), Placeholder: "╱╱"},
	}
}

// MaskText is text with masks applied, in order.
func MaskText(text string, masks ...Mask) string {
	for _, m := range masks {
		text = m.Pattern.ReplaceAllLiteralString(text, m.Placeholder)
	}
	return text
}
