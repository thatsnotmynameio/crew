package harness

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The names of a scenario's artifacts in the test's artifact directory.
const (
	artifactLogs       = "crew-logs"
	artifactStdout     = "crew.stdout"
	artifactStderr     = "crew.stderr"
	artifactScreen     = "screen.txt"
	artifactCalls      = "gh-calls.txt"
	artifactViolations = "unknown-calls.txt"
	// logsDir is where crew writes its logs, relative to the repository's
	// root.
	logsDir = ".crew/logs"
)

// saveArtifacts copies what explains a failed scenario to the test's
// artifact directory: crew's .crew/logs, its output or last screen, every
// gh call the fake GitHub received and the calls the doubles did not know.
// An artifact that cannot be saved is logged; it does not fail the test.
func (s *Scenario) saveArtifacts() {
	dir := s.tb.ArtifactDir()
	files := map[string]string{
		artifactCalls:      lines(s.GitHub.CallLog()),
		artifactViolations: lines(s.server.Violations()),
		artifactStdout:     s.Stdout(),
		artifactStderr:     s.Stderr(),
	}
	if s.screen != nil {
		files[artifactScreen] = s.screen.Text() + "\n"
	}
	for name, content := range files {
		if content == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), filePerm); err != nil {
			s.tb.Logf("save the artifact %s: %v", name, err)
		}
	}
	if s.Repo == "" {
		return
	}
	if err := copyLogs(filepath.Join(s.Repo, logsDir), filepath.Join(dir, artifactLogs)); err != nil {
		s.tb.Logf("save crew's logs: %v", err)
	}
}

// copyLogs copies the directory from, when it exists, to to.
func copyLogs(from, to string) error {
	if _, err := os.Stat(from); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err := os.CopyFS(to, os.DirFS(from)); err != nil {
		return fmt.Errorf("copy %s: %w", from, err)
	}
	return nil
}

// lines joins items one per line, "" for none.
func lines(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return strings.Join(items, "\n") + "\n"
}
