package fileline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppendPutsDataOnALineOfItsOwn(t *testing.T) {
	tests := []struct {
		name   string
		before string
		want   string
	}{
		{name: "empty file", before: "", want: "new\n"},
		{name: "after a whole line", before: "old\n", want: "old\nnew\n"},
		{name: "after a line cut short", before: "ol", want: "ol\nnew\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "lines")
			if err := os.WriteFile(path, []byte(tt.before), 0o600); err != nil {
				t.Fatal(err)
			}
			appendTo(t, path, "new")
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("file = %q, want %q", got, tt.want)
			}
		})
	}
}

// appendTo appends data to the file at path through Append, as the run
// journal and the session logs do.
func appendTo(t *testing.T, path, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	err = Append(f, []byte(data))
	if closeErr := f.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
}

func TestOpenCreatesTheFileAndItsDirectoryForAppending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "runs.jsonl")
	f, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := Append(f, []byte("first")); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	appendTo(t, path, "second")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "first\nsecond\n" {
		t.Errorf("file = %q, want both lines", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != filePerm {
		t.Errorf("file permission = %o, want %o", perm, filePerm)
	}
}

func TestOpenFailsNamingWhatItCouldNotDo(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "logs")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(filepath.Join(notADir, "runs.jsonl"))
	if err == nil || !strings.Contains(err.Error(), "create the log directory") {
		t.Errorf("Open under a file = %v, want an error creating the directory", err)
	}
	_, err = Open(dir)
	if err == nil || !strings.Contains(err.Error(), "open for appending") {
		t.Errorf("Open of a directory = %v, want an error opening for appending", err)
	}
}

func TestAppendFailsNamingWhatItCouldNotDo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lines")
	if err := os.WriteFile(path, []byte("cut"), 0o600); err != nil {
		t.Fatal(err)
	}
	closed, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	err = Append(closed, []byte("new"))
	if err == nil || !strings.Contains(err.Error(), "find the end of the last line") {
		t.Errorf("Append to a closed file = %v, want an error finding the end of the last line", err)
	}
	readOnly, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = readOnly.Close() }()
	err = Append(readOnly, []byte("new"))
	if err == nil || !strings.Contains(err.Error(), "end the last line") {
		t.Errorf("Append to a read-only file = %v, want an error ending the last line", err)
	}
}
