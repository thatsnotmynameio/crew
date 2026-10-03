package mates

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testMate returns a complete mate of the owner, holding the test key.
func testMate(owner string) Mate {
	return Mate{
		Name: "tester", Owner: owner, OwnerID: 42, AppID: 7, ClientID: "Iv23client",
		Slug: "crew-tester", AppName: "crew-tester", HTMLURL: "https://github.com/apps/crew-tester",
		BotLogin: "crew-tester[bot]", CreatedAt: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC),
		PrivateKey: pkcs1Key(),
	}
}

// mode returns the permission bits of path.
func mode(t *testing.T, path string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	return info.Mode().Perm()
}

func TestSavedMateLoadsBackFromItsOwnersDirectory(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root)
	m := testMate("ThatsNotMyNameIO")
	if err := s.Save(m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path := filepath.Join(root, "thatsnotmynameio", "tester.json")
	if got := s.Path("ThatsNotMyNameIO", "tester"); got != path {
		t.Errorf("Path = %s, want %s", got, path)
	}
	if got := mode(t, path); got != 0o600 {
		t.Errorf("file mode = %v, want 0600", got)
	}
	if got := mode(t, filepath.Dir(path)); got != 0o700 {
		t.Errorf("directory mode = %v, want 0700", got)
	}
	for _, owner := range []string{"ThatsNotMyNameIO", "thatsnotmynameio"} {
		got, err := s.Load(owner, "tester")
		if err != nil {
			t.Fatalf("Load(%s): %v", owner, err)
		}
		if got != m {
			t.Errorf("Load(%s) = %+v, want %+v", owner, got, m)
		}
	}
}

func TestLoadingAMissingMateSaysThereIsNone(t *testing.T) {
	_, err := NewStore(t.TempDir()).Load("thatsnotmynameio", "tester")
	if !errors.Is(err, ErrNoMate) {
		t.Errorf("Load = %v, want ErrNoMate", err)
	}
}

func TestLoadingAnInvalidMateNamesItsFileNotItsContents(t *testing.T) {
	for name, tc := range map[string]struct{ content, want string }{
		"corrupt JSON": {`{"private_key": "the key secret`, "not valid JSON"},
		"no key":       {`{"name": "tester", "app_id": 7}`, "no private key"},
		"no app id":    {`{"name": "tester", "private_key": "the key secret"}`, "no app id"},
	} {
		t.Run(name, func(t *testing.T) {
			s := NewStore(t.TempDir())
			path := s.Path("thatsnotmynameio", "tester")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := s.Load("thatsnotmynameio", "tester")
			if _, ok := errors.AsType[*EnvError](err); !ok {
				t.Fatalf("Load = %v, want an *EnvError", err)
			}
			msg := err.Error()
			if !strings.Contains(msg, path) || !strings.Contains(msg, tc.want) || strings.Contains(msg, "secret") {
				t.Errorf("Load = %q, want %q and the path %s without the file's contents", msg, tc.want, path)
			}
		})
	}
}

func TestLoadingAnUnreadableMateIsAnEnvironmentError(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := os.MkdirAll(s.Path("thatsnotmynameio", "tester"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := s.Load("thatsnotmynameio", "tester")
	if _, ok := errors.AsType[*EnvError](err); !ok || errors.Is(err, ErrNoMate) {
		t.Errorf("Load of a directory = %v, want an *EnvError that is not ErrNoMate", err)
	}
}

func TestSavingOverAnExistingMateFailsAndKeepsTheFirst(t *testing.T) {
	s := NewStore(t.TempDir())
	first := testMate("thatsnotmynameio")
	if err := s.Save(first); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path := s.Path("thatsnotmynameio", "tester")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.AppID = 8
	if err := s.Save(second); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("second Save = %v, want an error saying the mate already exists", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the second Save changed the first mate's file")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the owner's directory holds %d entries, want only tester.json", len(entries))
	}
}

func TestSavingWhereNoDirectoryCanBeMadeFails(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewStore(file).Save(testMate("thatsnotmynameio")); err == nil {
		t.Error("Save under a file = nil, want an error")
	}
}

func TestDefaultStoreLivesInTheUserConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	s, err := DefaultStore()
	if err != nil {
		t.Fatalf("DefaultStore: %v", err)
	}
	if want := filepath.Join(dir, "crew", "mates", "o", "n.json"); s.Path("o", "n") != want {
		t.Errorf("Path = %s, want %s", s.Path("o", "n"), want)
	}
}

func TestDefaultStoreWithoutAConfigDirIsAnEnvironmentError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	_, err := DefaultStore()
	if _, ok := errors.AsType[*EnvError](err); !ok {
		t.Errorf("DefaultStore = %v, want an *EnvError", err)
	}
}
