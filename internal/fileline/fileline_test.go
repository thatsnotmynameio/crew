package fileline

import (
	"os"
	"path/filepath"
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
