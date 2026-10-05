package codex

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// isolateGit keeps the developer's git config and any enclosing repository
// out of the test's git calls.
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	// A git hook running the tests sets these to the enclosing repository.
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR"} {
		if _, ok := os.LookupEnv(k); ok {
			t.Setenv(k, "")
			_ = os.Unsetenv(k)
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

// git runs git in dir with a fixed identity.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=crew test", "-c", "user.email=test@example.com",
		"-c", "commit.gpgsign=false"}, args...)
	cmd := exec.CommandContext(t.Context(), "git", full...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %q in %s: %v: %s", args, dir, err, out)
	}
}

// repository returns a repository with one commit and a worktree linked to
// it under .crew/worktrees, as crew's git workspace makes them, with their
// symlinks resolved.
func repository(t *testing.T) (string, string) {
	t.Helper()
	isolateGit(t)
	main, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git(t, main, "init", "--quiet", "--initial-branch=main")
	git(t, main, "commit", "--quiet", "--allow-empty", "--message=first")
	worktree := filepath.Join(main, ".crew", "worktrees", "issue-4-review")
	git(t, main, "worktree", "add", "--quiet", "-b", "crew/issue-4-review", worktree)
	return main, worktree
}

func TestGitDirsOfALinkedWorktreeAreItsOwnAndTheCommonOne(t *testing.T) {
	main, worktree := repository(t)

	got, err := gitDirs(t.Context(), (&proc.Group{}).Run, worktree)
	if err != nil {
		t.Fatalf("gitDirs: %v", err)
	}

	want := []string{filepath.Join(main, ".git", "worktrees", "issue-4-review"), filepath.Join(main, ".git")}
	if !slices.Equal(got, want) {
		t.Errorf("gitDirs = %q, want %q", got, want)
	}
}

func TestGitDirsOfTheMainCheckoutAreItsGitDirTwice(t *testing.T) {
	main, _ := repository(t)

	got, err := gitDirs(t.Context(), (&proc.Group{}).Run, main)
	if err != nil {
		t.Fatalf("gitDirs: %v", err)
	}

	if dir := filepath.Join(main, ".git"); !slices.Equal(got, []string{dir, dir}) {
		t.Errorf("gitDirs = %q, want %s twice", got, dir)
	}
}

func TestGitDirsOutsideARepositoryNameTheWorkspace(t *testing.T) {
	isolateGit(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))

	_, err := gitDirs(t.Context(), (&proc.Group{}).Run, dir)

	if err == nil || !strings.Contains(err.Error(), dir) {
		t.Errorf("gitDirs = %v, want an error naming %s", err, dir)
	}
}

func TestGitDirsGiveUpOnAGitThatHangs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		hang := func(ctx context.Context, _ proc.Command) (proc.Output, error) {
			<-ctx.Done()
			return proc.Output{}, ctx.Err()
		}

		_, err := gitDirs(t.Context(), hang, "/work")

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("gitDirs = %v, want it to give up at its deadline", err)
		}
	})
}

func TestGitDirsRefuseOutputThatIsNotTwoDirs(t *testing.T) {
	odd := func(context.Context, proc.Command) (proc.Output, error) {
		return proc.Output{Stdout: []byte("/repo/.git\n")}, nil
	}

	if _, err := gitDirs(t.Context(), odd, "/work"); err == nil {
		t.Error("gitDirs accepted one dir, want an error")
	}
}
