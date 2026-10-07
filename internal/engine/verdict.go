package engine

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// maxVerdict is how much of a verdict file the engine reads: far more than
// a verdict name on its first line needs.
const maxVerdict = 4096

// verdictFile is the file one session may write its verdict to, alone in a
// private directory of its own (KTD8).
type verdictFile struct {
	dir, file string
}

// newVerdictFile makes a new private directory, readable by you alone,
// outside the worktree and .crew/logs/, holding an empty verdict file. A
// new directory for each session means an earlier session cannot plant a
// verdict for a later one.
func newVerdictFile() (verdictFile, error) {
	dir, err := os.MkdirTemp("", "crew-verdict-")
	if err != nil {
		return verdictFile{}, fmt.Errorf("create the session's verdict file: %w", err)
	}
	v := verdictFile{dir: dir, file: filepath.Join(dir, "verdict")}
	if err := os.WriteFile(v.file, nil, sessionFilePerm); err != nil {
		v.remove()
		return verdictFile{}, fmt.Errorf("create the session's verdict file: %w", err)
	}
	return v, nil
}

// read reads the verdict file once, at most maxVerdict bytes of it, then
// removes its directory, and returns the verdict the session reported
// (KTD-S4): none for an empty or missing file, the first word for a file
// whose first word is a verdict name, and unreadable for any other text,
// or for text holding a control character other than spacing, so neither
// an escape sequence nor a cleaned-up name reaches crew.
func (v verdictFile) read() crew.VerdictReport {
	defer v.remove()
	f, err := os.Open(v.file)
	if errors.Is(err, fs.ErrNotExist) {
		return crew.NoVerdictReported{}
	}
	if err != nil {
		return crew.VerdictUnreadable{}
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxVerdict))
	if err != nil {
		return crew.VerdictUnreadable{}
	}
	return parseVerdict(string(data))
}

// spacing is the control characters a verdict file may hold: those that
// separate words and lines.
const spacing = "\t\n\v\f\r"

// parseVerdict returns the verdict text, a verdict file's, reports.
func parseVerdict(text string) crew.VerdictReport {
	if strings.ContainsFunc(text, func(r rune) bool { return unicode.IsControl(r) && !strings.ContainsRune(spacing, r) }) {
		return crew.VerdictUnreadable{}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return crew.NoVerdictReported{}
	}
	verdict, err := crew.ParseVerdict(words[0])
	if err != nil {
		return crew.VerdictUnreadable{}
	}
	return crew.VerdictReported{Verdict: verdict}
}

// remove removes the verdict file's directory. What a failed removal
// leaves is in the system's temporary directory, readable by you alone.
func (v verdictFile) remove() {
	_ = os.RemoveAll(v.dir)
}
