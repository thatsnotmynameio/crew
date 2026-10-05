package bots

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBrowserCommandIsOpenOnMacOSAndXdgOpenOnLinux(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "open", "linux": "xdg-open"} {
		if got := browserCommand(goos); got != want {
			t.Errorf("browserCommand(%s) = %q, want %q", goos, got, want)
		}
	}
}

func TestOpenBrowserStartsTheOpenerWithTheURL(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "opened")
	// PATH holds only the opener, so the script uses only the shell.
	script := "#!/bin/sh\necho \"$1\" > \"$CREW_TEST_OPENED\"\n"
	if err := os.WriteFile(filepath.Join(dir, browserCommand(runtime.GOOS)), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CREW_TEST_OPENED", out)
	if err := OpenBrowser("http://127.0.0.1:1234/"); err != nil {
		t.Fatalf("OpenBrowser: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(out)
		if err == nil && strings.HasSuffix(string(data), "\n") {
			if got := strings.TrimSpace(string(data)); got != "http://127.0.0.1:1234/" {
				t.Errorf("the opener got %q, want the URL", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the opener never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOpenBrowserWithoutAnOpenerFails(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := OpenBrowser("http://127.0.0.1:1234/"); err == nil {
		t.Error("OpenBrowser without an opener = nil error, want one")
	}
}
