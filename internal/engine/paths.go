package engine

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fileline"
)

// logDir is where session logs go, relative to the repository root (KTD12).
const logDir = ".crew/logs"

// JournalPath is where the run journal goes, relative to the repository
// root, with slashes (KTD12). It sits with the logs its events point to,
// under a directory crew's ignore rules already cover.
const JournalPath = logDir + "/runs.jsonl"

// logPath returns the repository-relative path of the log of the rule runs
// working in workspace. A log holds every action of its run, each after a
// marker line naming it, and a resumed run's output goes after the earlier
// run's (R10, KTD6).
func logPath(workspace crew.WorkspaceName) string {
	return logDir + "/" + string(workspace) + ".log"
}

// sessionFiles returns the repository-relative paths of the two files kept
// beside the log at the repository-relative path log: the prompt the run's
// latest session started with and its last message (KTD22). They stay on
// this machine, under the directory crew's ignore rules cover.
func sessionFiles(log string) (string, string) {
	base := strings.TrimSuffix(log, ".log")
	return base + ".prompt", base + ".last-message"
}

// keepSession keeps prompt and lastMessage, the latest session's, beside
// the log at the repository-relative path log, for the shell actions and
// route steps after it, also after a restart (AE11). When it cannot write
// them, it removes both, so no script reads an earlier session's words as
// this one's.
func (e *Engine) keepSession(log, prompt, lastMessage string) {
	promptFile, lastFile := sessionFiles(log)
	for rel, text := range map[string]string{promptFile: prompt, lastFile: lastMessage} {
		if err := os.WriteFile(e.abs(rel), []byte(text), sessionFilePerm); err != nil {
			e.clearSession(log)
			return
		}
	}
}

// clearSession removes the files kept beside the log at the
// repository-relative path log. What a failed removal leaves is read only
// by a script after a session, which keeps its own first.
func (e *Engine) clearSession(log string) {
	promptFile, lastFile := sessionFiles(log)
	_ = os.Remove(e.abs(promptFile))
	_ = os.Remove(e.abs(lastFile))
}

// session returns the prompt and the last message of the session named
// name, as kept beside the log at the repository-relative path log: both
// empty when name is, before any session (KTD-S11), or when it kept none.
func (e *Engine) session(log string, name crew.ActionName) (string, string, error) {
	if name == "" {
		return "", "", nil
	}
	promptFile, lastFile := sessionFiles(log)
	prompt, err := readKept(e.abs(promptFile))
	if err != nil {
		return "", "", err
	}
	lastMessage, err := readKept(e.abs(lastFile))
	if err != nil {
		return "", "", err
	}
	return prompt, lastMessage, nil
}

// readKept returns the text of the file at path, or "" when there is none.
func readKept(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: a file crew keeps beside a log of its own
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the session's words: %w", err)
	}
	return string(data), nil
}

// sessionFilePerm is the permission of the files kept beside a log, which
// hold what a session read or wrote: yours alone.
const sessionFilePerm = 0o600

// abs returns the absolute path of the repository-relative path rel.
func (e *Engine) abs(rel string) string {
	return filepath.Join(e.cfg.Root, filepath.FromSlash(rel))
}

// logFromDir returns the path of the log at the repository-relative path
// log, relative to dir, so a session working in dir can open it; "" when
// there is no such path.
func (e *Engine) logFromDir(dir, log string) string {
	rel, err := filepath.Rel(dir, e.abs(log))
	if err != nil {
		return ""
	}
	return filepath.ToSlash(rel)
}

// openLog opens the log at the repository-relative path rel for appending,
// creating it and its directory as needed.
func (e *Engine) openLog(rel string) (*os.File, error) {
	f, err := e.openAppend(rel)
	if err != nil {
		return nil, fmt.Errorf("open the session log: %w", err)
	}
	return f, nil
}

// openAppend opens the file at the repository-relative path rel for reading
// and appending, creating it and its directory as needed.
func (e *Engine) openAppend(rel string) (*os.File, error) {
	return fileline.Open(e.abs(rel))
}

// scrub shortens the local paths in text before it enters the core: the
// repository root becomes . and the home directory ~ (KTD12). A tool's
// stderr or a session's last message often names absolute paths, and
// failure reports end up on public issues. The root goes first, as it
// usually sits inside the home directory. It also redacts GitHub tokens and
// PEM private keys, a session acting as a bot holding one, before anything
// cuts the text, so no cut leaves part of a token without its prefix.
func (e *Engine) scrub(text string) string {
	text = privateKey.ReplaceAllString(text, "[redacted private key]")
	text = githubToken.ReplaceAllString(text, "[redacted token]")
	return replaceDir(replaceDir(text, e.cfg.Root, "."), e.cfg.Home, "~")
}

// scrubAndStrip is text scrubbed, stripped of its control characters
// (crew.StripControls) and scrubbed again, for the constructors of the text
// crew shows from a session (KTD3). The first scrub sees the raw bytes, so a
// token after a control byte, as in foo\x00ghp_…, still starts at a word
// boundary. The second closes what the strip itself opens: removing an
// escape sequence can join a token's parts, as gh\x1b[0mp_… becomes ghp_….
func (e *Engine) scrubAndStrip(text string) string {
	scrubbed := e.scrub(text)
	stripped := crew.StripControls(scrubbed)
	if stripped == scrubbed {
		// Nothing was stripped, so nothing was joined: a second scrub
		// would find nothing more. This keeps a plain text, as most
		// sessions' words are at every refresh, to one pass.
		return scrubbed
	}
	return e.scrub(stripped)
}

// githubToken matches GitHub's tokens by their prefixes: personal, OAuth,
// user-to-server, installation and refresh tokens, and fine-grained
// personal access tokens. A stateless installation token, ghs_APPID_JWT,
// holds base64url segments joined by dots; a dot is matched only between
// segments, so one ending a sentence stays.
var githubToken = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)*|github_pat_[A-Za-z0-9_]+)`)

// privateKey matches a PEM private key block, or its start up to the end
// of the text when the text ends inside it.
var privateKey = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----(?s:.*?)(?:-----END [A-Z ]*PRIVATE KEY-----|\z)`)

// maxSaid is how many characters of what a session last said reach the core.
const maxSaid = 200

// lastWords keeps the last maxSaid characters of text, starting a cut text
// with an ellipsis: the end of what a session says is the newest.
func lastWords(text string) string {
	runes := []rune(text)
	if len(runes) <= maxSaid {
		return text
	}
	return "…" + string(runes[len(runes)-maxSaid+1:])
}

// minDir is the length of the shortest dir replaceDir replaces: shorter is
// empty or the filesystem root.
const minDir = 2

// replaceDir replaces dir in text with short wherever it appears as a whole
// path prefix: not preceded by a path byte, and not followed by a name
// byte, so /home/jo leaves /home/joe and /mnt/home/jo alone. Dots ending a
// sentence do not continue the name: /repo. and /repo... are /repo, while
// /repo.git is another path. An empty dir, or the filesystem root, is left
// as is.
func replaceDir(text, dir, short string) string {
	if len(dir) < minDir {
		return text
	}
	var b strings.Builder
	for {
		i := strings.Index(text, dir)
		if i < 0 {
			break
		}
		end := i + len(dir)
		if (i == 0 || !pathByte(text[i-1])) && endsName(text[end:]) {
			b.WriteString(text[:i])
			b.WriteString(short)
		} else {
			b.WriteString(text[:end])
		}
		text = text[end:]
	}
	b.WriteString(text)
	return b.String()
}

// endsName reports whether rest, the text right after a path, ends the
// path's last name: past any dots, it is empty or starts with a byte that
// cannot continue a name.
func endsName(rest string) bool {
	rest = strings.TrimLeft(rest, ".")
	return rest == "" || !nameByte(rest[0])
}

// nameByte reports whether c can continue a file name. Bytes of multi-byte
// UTF-8 characters count as name bytes.
func nameByte(c byte) bool {
	return c >= 0x80 || c == '.' || c == '-' || c == '_' ||
		('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9')
}

// pathByte reports whether c can appear inside a path.
func pathByte(c byte) bool {
	return c == '/' || nameByte(c)
}
