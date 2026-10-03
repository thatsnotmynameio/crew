package mates

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// dirPerm is the permission of the mates' directories. A mate's file holds
// its private key, so only the boss's own OS user reads it: the directories
// are 0700, and os.CreateTemp makes the file 0600.
const dirPerm = 0o700

// ErrNoMate is the error Store.Load wraps when the owner has no mate of
// that name on this machine.
var ErrNoMate = errors.New("no such mate")

// Mate is a crew mate: a private GitHub App and the key to sign as it. Its
// JSON is the mate's file. It holds no client or webhook secret, which
// nothing uses.
type Mate struct {
	// Name is the mate's name, as given to crew mates create.
	Name string `json:"name"`
	// Owner is the login of the account that owns the app.
	Owner string `json:"owner"`
	// OwnerID is that account's id.
	OwnerID int64 `json:"owner_id"`
	// AppID is the app's id.
	AppID int64 `json:"app_id"`
	// ClientID is the app's client id, the issuer of its JWTs.
	ClientID string `json:"client_id"`
	// Slug is the app's slug, the URL form of its name.
	Slug string `json:"slug"`
	// AppName is the name GitHub created the app with.
	AppName string `json:"app_name"`
	// HTMLURL is the app's page on GitHub.
	HTMLURL string `json:"html_url"`
	// BotLogin is the login the app acts as, such as crew-tester[bot].
	BotLogin string `json:"bot_login"`
	// CreatedAt is when GitHub created the app.
	CreatedAt time.Time `json:"created_at"`
	// PrivateKey is the app's private key. It never prints.
	PrivateKey PrivateKey `json:"private_key"`
}

// Store keeps mates as files under its root, one per mate at
// <root>/<owner>/<name>.json, the owner's login in lower case because
// GitHub logins ignore case.
type Store struct {
	root string
}

// NewStore returns the store rooted at root.
func NewStore(root string) *Store {
	return &Store{root: root}
}

// DefaultStore returns the store in the boss's user config directory,
// <user config dir>/crew/mates, outside any repository. Without a user
// config directory it returns an EnvError.
func DefaultStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, envErrorf("find where to keep mates: %w", err)
	}
	return NewStore(filepath.Join(dir, "crew", "mates")), nil
}

// Path returns the path of the file of owner's mate called name.
func (s *Store) Path(owner, name string) string {
	return filepath.Join(s.root, strings.ToLower(owner), name+".json")
}

// Load returns owner's mate called name. When there is none it returns an
// error wrapping ErrNoMate; a file it cannot read, or one that is not a
// mate, is an EnvError naming the file but never quoting it.
func (s *Store) Load(owner, name string) (Mate, error) {
	path := s.Path(owner, name)
	data, err := os.ReadFile(path) //nolint:gosec // the path is the store's, built from an owner login and a mate name
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Mate{}, fmt.Errorf("mate %s of %s: %w", name, owner, ErrNoMate)
	case err != nil:
		return Mate{}, envErrorf("read the mate: %w", err)
	}
	var m Mate
	if json.Unmarshal(data, &m) != nil {
		// The decoder's message can quote the file, and the file holds a key.
		return Mate{}, envErrorf("the mate file %s is not valid JSON", path)
	}
	switch {
	case m.PrivateKey == "":
		return Mate{}, envErrorf("the mate file %s has no private key", path)
	case m.AppID == 0:
		return Mate{}, envErrorf("the mate file %s has no app id", path)
	}
	return m, nil
}

// Save writes m's file, creating its directories with mode 0700 and the
// file with mode 0600. It writes a temporary file and links it into place
// only when the mate has no file yet: a crash leaves no half-written mate,
// and a mate is never overwritten.
func (s *Store) Save(m Mate) error {
	path := s.Path(m.Owner, m.Name)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("save the mate: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ") //nolint:gosec // G117: the mate's file is where its key is kept (R9)
	if err != nil {
		return fmt.Errorf("save the mate: %w", err)
	}
	tmp, err := writeTemp(dir, m.Name, data)
	if err != nil {
		return fmt.Errorf("save the mate: %w", err)
	}
	defer func() { _ = os.Remove(tmp) }()
	if err := os.Link(tmp, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("save the mate: the mate %s of %s already exists at %s", m.Name, m.Owner, path)
		}
		return fmt.Errorf("save the mate: %w", err)
	}
	return nil
}

// writeTemp writes data to a new file of mode 0600 in dir, named after the
// mate called name, and returns its path.
func writeTemp(dir, name string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, "."+name+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("create a temporary file: %w", err)
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("write a temporary file: %w", err)
	}
	return f.Name(), nil
}
