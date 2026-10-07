package engine

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// A session's verdict file that is gone reports no verdict, and one crew
// cannot read reports an unreadable verdict; either way its directory goes.
func TestAVerdictFileTheSessionReplacedIsReadSafely(t *testing.T) {
	tests := []struct {
		name    string
		replace func(file string) error
		want    crew.VerdictReport
	}{
		{name: "removed", replace: os.Remove, want: crew.NoVerdictReported{}},
		{name: "a directory", replace: func(file string) error {
			return errors.Join(os.Remove(file), os.Mkdir(file, 0o700))
		}, want: crew.VerdictUnreadable{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			v, err := newVerdictFile()
			if err != nil {
				t.Fatal(err)
			}
			if err := tt.replace(v.file); err != nil {
				t.Fatal(err)
			}
			if got := v.read(); got != tt.want {
				t.Errorf("read = %#v, want %#v", got, tt.want)
			}
			if _, err := os.Stat(v.dir); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("verdict directory after read: %v, want it gone", err)
			}
		})
	}
}

// Without a temporary directory to make one in, a session gets no verdict
// file, and crew says so.
func TestAVerdictFileNeedsATemporaryDirectory(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	if v, err := newVerdictFile(); err == nil {
		t.Errorf("newVerdictFile = %#v, nil, want an error", v)
	}
}
