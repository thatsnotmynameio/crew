package engine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestScrubShortensRootAndHomeOnlyAtWholePathBoundaries(t *testing.T) {
	const root, home = "/home/jo/repo", "/home/jo"
	tests := []struct {
		name       string
		root, home string
		text, want string
	}{
		{"home", root, home, "see /home/jo/.gitconfig", "see ~/.gitconfig"},
		{"a longer name is another directory", root, home, "see /home/joe/.gitconfig", "see /home/joe/.gitconfig"},
		{"a path inside another is not home", root, home, "see /mnt/home/jo/x", "see /mnt/home/jo/x"},
		{"root inside home wins", root, home, "open /home/jo/repo/main.go", "open ./main.go"},
		{"empty home shortens nothing", "/srv/repo", "", "see /home/jo/x", "see /home/jo/x"},
		{"root at the end of the text", root, home, "cd /home/jo/repo", "cd ."},
		{"root followed by a slash", root, home, "in /home/jo/repo/", "in ./"},
		{"root ending a sentence", root, home, "failed in /home/jo/repo.", "failed in .."},
		{"root ending a sentence mid-text", root, home, "failed in /home/jo/repo. Retry.", "failed in .. Retry."},
		{"root ending a sentence in brackets", root, home, "(in /home/jo/repo.)", "(in ..)"},
		{"root trailed by an ellipsis", root, home, "working in /home/jo/repo...", "working in ...."},
		{"a dotted name is another path", root, home, "clone /home/jo/repo.git", "clone ~/repo.git"},
		{"home ending a sentence", root, home, "no config in /home/jo.", "no config in ~."},
		{"home ending a sentence before a space", root, home, "no config in /home/jo. Retry", "no config in ~. Retry"},
		{"a dotted name under home", root, home, "see /home/jo.bak", "see /home/jo.bak"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Engine{cfg: Config{Root: tt.root, Home: tt.home}}
			if got := e.scrub(tt.text); got != tt.want {
				t.Errorf("scrub(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestReplaceDirLeavesAnEmptyDirOrTheFilesystemRootAlone(t *testing.T) {
	for _, dir := range []string{"", "/"} {
		if got := replaceDir("see /home/jo", dir, "~"); got != "see /home/jo" {
			t.Errorf("replaceDir with dir %q = %q, want the text unchanged", dir, got)
		}
	}
}

func TestScrubRedactsTokensBeforeTheCut(t *testing.T) {
	e := &Engine{cfg: Config{Root: "/home/jo/repo", Home: "/home/jo"}}
	// In a 260-character text, the token starts at 59, so the cut keeping
	// the last 199 after its ellipsis, from 61, would split its prefix.
	token := "ghs_" + strings.Repeat("A", 36)
	text := strings.Repeat("x", 57) + "t=" + token + strings.Repeat("y", 161)
	if cut := lastWords(text); !strings.HasPrefix(cut, "…s_AAAA") {
		t.Fatalf("lastWords = %q, want the cut inside the token's prefix", cut)
	}
	got := lastWords(e.scrub(text))
	if strings.Contains(got, "AAAA") || !strings.Contains(got, "[redacted token]") {
		t.Errorf("lastWords(scrub) = %q, want the redaction and no part of the token", got)
	}
	for _, tok := range []string{"ghp_abc123", "gho_abc", "ghu_abc", "ghr_abc", "github_pat_11AB_cd"} {
		if got := e.scrub("token " + tok + " leaked"); got != "token [redacted token] leaked" {
			t.Errorf("scrub(%q) = %q, want it redacted", tok, got)
		}
	}
	// A stateless installation token, ghs_APPID_JWT, goes whole, and the
	// dot ending the sentence stays.
	stateless := "ghs_12345_eyJhbGciOiJSUzI1NiJ9.eyJpc3MiOiIxMjM0NSJ9-_x.c2lnbmF0dXJl-_"
	if got := e.scrub("token " + stateless + "."); got != "token [redacted token]." {
		t.Errorf("scrub(%q) = %q, want it redacted whole", stateless, got)
	}
	if got := e.scrub("a ghost_town and highs_"); got != "a ghost_town and highs_" {
		t.Errorf("scrub = %q, want words that are no tokens left alone", got)
	}
}

func TestScrubRedactsPrivateKeys(t *testing.T) {
	e := &Engine{}
	//nolint:gosec // G101: a fake key, which scrub must redact
	key := "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\nIBAAK\n-----END RSA PRIVATE KEY-----" // gitleaks:allow
	if got := e.scrub("cat key.pem:\n" + key + "\ndone"); got != "cat key.pem:\n[redacted private key]\ndone" {
		t.Errorf("scrub = %q, want the block redacted", got)
	}
	cut := "head: -----BEGIN PRIVATE KEY-----\nMIIEow" // gitleaks:allow
	if got := e.scrub(cut); got != "head: [redacted private key]" {
		t.Errorf("scrub = %q, want a cut block redacted to the end", got)
	}
}

// KTD22: the latest session's prompt and last message are kept beside the
// run's log, private to you, and read back only for a script after a
// session.
func TestTheLatestSessionsWordsAreKeptBesideTheRunsLog(t *testing.T) {
	root := t.TempDir()
	e := &Engine{cfg: Config{Root: root}}
	if err := os.MkdirAll(filepath.Join(root, ".crew", "logs"), 0o700); err != nil {
		t.Fatal(err)
	}

	wantSession(t, e, "lfg", "", "")
	e.keepSession(keptLog, "/lfg #9", "PR #10 is open.")
	for _, name := range []string{"issue-9-lfg.prompt", "issue-9-lfg.last-message"} {
		info, err := os.Stat(filepath.Join(root, ".crew", "logs", name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s = %v, %v, want a file only you can read", name, info, err)
		}
	}
	wantSession(t, e, "lfg", "/lfg #9", "PR #10 is open.")
	wantSession(t, e, "", "", "")
	e.clearSession(keptLog)
	wantSession(t, e, "lfg", "", "")
}

// keptLog is the log of the run in the workspace issue-9-lfg.
const keptLog = ".crew/logs/issue-9-lfg.log"

// wantSession fails unless e reads prompt and last as the words of the
// session named name kept beside the log of issue-9-lfg.
func wantSession(t *testing.T, e *Engine, name crew.ActionName, prompt, last string) {
	t.Helper()
	gotPrompt, gotLast, err := e.session(keptLog, name)
	if err != nil || gotPrompt != prompt || gotLast != last {
		t.Errorf("session(%q) = %q, %q, %v, want %q, %q", name, gotPrompt, gotLast, err, prompt, last)
	}
}

// A session whose words cannot be kept leaves none of an earlier session's
// for the scripts after it.
func TestWordsThatCannotBeKeptClearTheEarlierOnes(t *testing.T) {
	root := t.TempDir()
	e := &Engine{cfg: Config{Root: root}}
	logs := filepath.Join(root, ".crew", "logs")
	if err := os.MkdirAll(filepath.Join(logs, "issue-9-lfg.last-message"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "issue-9-lfg.prompt"), []byte("an earlier prompt"), 0o600); err != nil {
		t.Fatal(err)
	}

	e.keepSession(keptLog, "/lfg #9", "PR #10 is open.")

	if _, err := os.Stat(filepath.Join(logs, "issue-9-lfg.prompt")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("prompt kept after a failed write: %v, want it gone", err)
	}
}

// The words of a session that crew kept but cannot read are an error, not
// words a script would take as the session's.
func TestKeptWordsThatCannotBeReadAreAnError(t *testing.T) {
	for _, name := range []string{"issue-9-lfg.prompt", "issue-9-lfg.last-message"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			e := &Engine{cfg: Config{Root: root}}
			if err := os.MkdirAll(filepath.Join(root, ".crew", "logs", name), 0o700); err != nil {
				t.Fatal(err)
			}
			if prompt, last, err := e.session(keptLog, "lfg"); err == nil {
				t.Errorf("session = %q, %q, nil, want an error", prompt, last)
			}
		})
	}
}
