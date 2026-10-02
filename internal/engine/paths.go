package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// logDir is where session logs go, relative to the repository root (KTD12).
const logDir = ".crew/logs"

// logPath returns the repository-relative path of the log of the session
// running in workspace. Workspace names are unique, so a log holds exactly
// one session.
func logPath(workspace string) string {
	return logDir + "/" + workspace + ".log"
}

// openLog opens the log at the repository-relative path rel for appending,
// creating it and its directory as needed.
func (e *Engine) openLog(rel string) (*os.File, error) {
	path := filepath.Join(e.cfg.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create the log directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the session log: %w", err)
	}
	return f, nil
}

// scrub shortens the local paths in text before it enters the core: the
// repository root becomes . and the home directory ~ (KTD12). A tool's
// stderr or a session's last message often names absolute paths, and
// failure reports end up on public issues. The root goes first, as it
// usually sits inside the home directory.
func (e *Engine) scrub(text string) string {
	return replaceDir(replaceDir(text, e.cfg.Root, "."), e.cfg.Home, "~")
}

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

// replaceDir replaces dir in text with with wherever it appears as a whole
// path prefix: not preceded by a path byte, and not followed by a name
// byte, so /home/jo leaves /home/joe and /mnt/home/jo alone. Dots ending a
// sentence do not continue the name: /repo. and /repo... are /repo, while
// /repo.git is another path. An empty dir, or the filesystem root, is left
// as is.
func replaceDir(text, dir, with string) string {
	if len(dir) < 2 {
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
			b.WriteString(with)
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
