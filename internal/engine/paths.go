package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// logDir is where session logs go, relative to the repository root (KTD12).
const logDir = ".crew/logs"

// The permissions of the log directory and of the files in it: logs hold
// what sessions printed, so only you read them.
const (
	logDirPerm  = 0o700
	logFilePerm = 0o600
)

// logPath returns the repository-relative path of the log of the sessions
// running in workspace. A log holds every session of its workspace: a
// resumed session's output goes after the failed run's (R10).
func logPath(workspace crew.WorkspaceName) string {
	return logDir + "/" + string(workspace) + ".log"
}

// logFromDir returns the path of the log at the repository-relative path
// log, relative to dir, so a session working in dir can open it; "" when
// there is no such path.
func (e *Engine) logFromDir(dir, log string) string {
	rel, err := filepath.Rel(dir, filepath.Join(e.cfg.Root, filepath.FromSlash(log)))
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
	path := filepath.Join(e.cfg.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), logDirPerm); err != nil {
		return nil, fmt.Errorf("create the log directory: %w", err)
	}
	//nolint:gosec // crew builds the path under .crew/logs
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, logFilePerm)

	if err != nil {
		return nil, fmt.Errorf("open for appending: %w", err)
	}
	return f, nil
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
