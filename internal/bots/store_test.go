package bots

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testBot returns a complete bot of the owner, holding the test key.
func testBot(owner string) Bot {
	return Bot{
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

func TestSavedBotLoadsBackFromItsOwnersDirectory(t *testing.T) {
	root := t.TempDir()
	s := NewStore(root, "")
	m := testBot("ThatsNotMyNameIO")
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
		got, from, err := s.Load(owner, "tester")
		if err != nil {
			t.Fatalf("Load(%s): %v", owner, err)
		}
		if got != m || from != path {
			t.Errorf("Load(%s) = %+v from %s, want %+v from %s", owner, got, from, m, path)
		}
	}
}

func TestLoadingAMissingBotSaysThereIsNone(t *testing.T) {
	_, _, err := NewStore(t.TempDir(), t.TempDir()).Load("thatsnotmynameio", "tester")
	if !errors.Is(err, ErrNoBot) {
		t.Errorf("Load = %v, want ErrNoBot", err)
	}
}

func TestLoadingAnInvalidBotNamesItsFileNotItsContents(t *testing.T) {
	for name, tc := range map[string]struct{ content, want string }{
		"corrupt JSON": {`{"private_key": "the key secret`, "not valid JSON"},
		"no key":       {`{"name": "tester", "app_id": 7}`, "no private key"},
		"no app id":    {`{"name": "tester", "private_key": "the key secret"}`, "no app id"},
	} {
		t.Run(name, func(t *testing.T) {
			s := NewStore(t.TempDir(), "")
			path := s.Path("thatsnotmynameio", "tester")
			writeBotFile(t, path, tc.content)
			_, _, err := s.Load("thatsnotmynameio", "tester")
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

func TestLoadingAnUnreadableBotIsAnEnvironmentError(t *testing.T) {
	s := NewStore(t.TempDir(), "")
	if err := os.MkdirAll(s.Path("thatsnotmynameio", "tester"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Load("thatsnotmynameio", "tester")
	if _, ok := errors.AsType[*EnvError](err); !ok || errors.Is(err, ErrNoBot) {
		t.Errorf("Load of a directory = %v, want an *EnvError that is not ErrNoBot", err)
	}
}

func TestSavingOverAnExistingBotFailsAndKeepsTheFirst(t *testing.T) {
	s := NewStore(t.TempDir(), "")
	first := testBot("thatsnotmynameio")
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
		t.Errorf("second Save = %v, want an error saying the bot already exists", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("the second Save changed the first bot's file")
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
	if err := NewStore(file, "").Save(testBot("thatsnotmynameio")); err == nil {
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
	if want := filepath.Join(dir, "crew", "bots", "o", "n.json"); s.Path("o", "n") != want {
		t.Errorf("Path = %s, want %s", s.Path("o", "n"), want)
	}
	// A bot saved by an older crew, under crew/mates, still loads.
	old := NewStore(filepath.Join(dir, "crew", "mates"), "")
	if err := old.Save(testBot("o")); err != nil {
		t.Fatal(err)
	}
	if _, from, err := s.Load("o", "tester"); err != nil || from != old.Path("o", "tester") {
		t.Errorf("Load = %s, %v; want the bot under crew/mates, from %s", from, err, old.Path("o", "tester"))
	}
}

// writeBotFile writes content as the bot file at path, making its
// directories.
func writeBotFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), filePerm); err != nil {
		t.Fatal(err)
	}
}

// oldAndNew returns a store rooted in a new temporary directory, its old
// root another, and a store of that old root alone, as an older crew
// wrote it.
func oldAndNew(t *testing.T) (*Store, *Store) {
	t.Helper()
	oldRoot := t.TempDir()
	return NewStore(t.TempDir(), oldRoot), NewStore(oldRoot, "")
}

func TestABotUnderTheOldRootLoadsFromThere(t *testing.T) {
	s, old := oldAndNew(t)
	m := testBot("thatsnotmynameio")
	if err := old.Save(m); err != nil {
		t.Fatal(err)
	}
	got, from, err := s.Load("thatsnotmynameio", "tester")
	if err != nil || got != m || from != old.Path("thatsnotmynameio", "tester") {
		t.Errorf("Load = %+v from %s, %v; want the old root's bot from %s", got, from, err,
			old.Path("thatsnotmynameio", "tester"))
	}
	if _, err := os.Stat(s.Path("thatsnotmynameio", "tester")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load moved or copied the bot to the new root: %v", err)
	}
}

func TestABotUnderBothRootsLoadsFromTheNewOne(t *testing.T) {
	s, old := oldAndNew(t)
	stale, current := testBot("thatsnotmynameio"), testBot("thatsnotmynameio")
	stale.AppID = 6
	if err := old.Save(stale); err != nil {
		t.Fatal(err)
	}
	if err := s.Save(current); err != nil {
		t.Fatal(err)
	}
	got, from, err := s.Load("thatsnotmynameio", "tester")
	if err != nil || got != current || from != s.Path("thatsnotmynameio", "tester") {
		t.Errorf("Load = app %d from %s, %v; want app %d from %s", got.AppID, from, err, current.AppID,
			s.Path("thatsnotmynameio", "tester"))
	}
}

func TestABadBotFileUnderTheOldRootIsNamedByItsPath(t *testing.T) {
	s, old := oldAndNew(t)
	path := old.Path("thatsnotmynameio", "tester")
	writeBotFile(t, path, "{")
	_, from, err := s.Load("thatsnotmynameio", "tester")
	if _, ok := errors.AsType[*EnvError](err); !ok || !strings.Contains(err.Error(), path) || from != path {
		t.Errorf("Load = %s, %v; want an *EnvError naming %s", from, err, path)
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
