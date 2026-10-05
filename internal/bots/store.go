package bots

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

// dirPerm is the permission of the bots' directories. A bot's file holds
// its private key, so only your own OS user reads it: the directories
// are 0700, and os.CreateTemp makes the file 0600.
const dirPerm = 0o700

// ErrNoBot is the error Store.Load wraps when the owner has no bot of
// that name on this machine.
var ErrNoBot = errors.New("no such bot")

// Bot is a crew bot: a private GitHub App and the key to sign as it. Its
// JSON is the bot's file. It holds no client or webhook secret, which
// nothing uses.
type Bot struct {
	// Name is the bot's name, as given to crew bots create.
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

// Store keeps bots as files under its root, one per bot at
// <root>/<owner>/<name>.json, the owner's login in lower case because
// GitHub logins ignore case. It also reads, never writes, the bots an
// older crew saved under its old root.
type Store struct {
	root, old string
}

// NewStore returns the store rooted at root that also reads the bots
// under old, or none when old is "".
func NewStore(root, old string) *Store {
	return &Store{root: root, old: old}
}

// DefaultStore returns the store in your user config directory,
// <user config dir>/crew/bots, outside any repository, which also reads
// the bots older crews saved in <user config dir>/crew/mates (KTD9).
// Without a user config directory it returns an EnvError.
func DefaultStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, envErrorf("find where to keep bots: %w", err)
	}
	return NewStore(filepath.Join(dir, "crew", "bots"), filepath.Join(dir, "crew", "mates")), nil
}

// Path returns the path Save writes the file of owner's bot called name
// to, under the store's root.
func (s *Store) Path(owner, name string) string {
	return botPath(s.root, owner, name)
}

// botPath returns the path of the file of owner's bot called name under
// root.
func botPath(root, owner, name string) string {
	return filepath.Join(root, strings.ToLower(owner), name+".json")
}

// Load returns owner's bot called name and the path of the file it read:
// the one under the store's root, else the one under its old root. When
// there is neither it returns an error wrapping ErrNoBot. A file it cannot
// read, or one that is not a bot, is an EnvError naming the file but never
// quoting it, returned with the file's path, so the message that tells you
// which file to delete names the one crew read.
func (s *Store) Load(owner, name string) (Bot, string, error) {
	for _, root := range []string{s.root, s.old} {
		if root == "" {
			continue
		}
		path := botPath(root, owner, name)
		data, err := os.ReadFile(path) //nolint:gosec // the path is the store's, built from an owner login and a bot name
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return Bot{}, path, envErrorf("read the bot: %w", err)
		}
		m, err := decodeBot(path, data)
		return m, path, err
	}
	return Bot{}, "", fmt.Errorf("bot %s of %s: %w", name, owner, ErrNoBot)
}

// decodeBot returns the bot whose file, at path, holds data.
func decodeBot(path string, data []byte) (Bot, error) {
	var m Bot
	if json.Unmarshal(data, &m) != nil {
		// The decoder's message can quote the file, and the file holds a key.
		return Bot{}, envErrorf("the bot file %s is not valid JSON", path)
	}
	switch {
	case m.PrivateKey == "":
		return Bot{}, envErrorf("the bot file %s has no private key", path)
	case m.AppID == 0:
		return Bot{}, envErrorf("the bot file %s has no app id", path)
	}
	return m, nil
}

// Save writes m's file under the store's root, creating its directories
// with mode 0700 and the file with mode 0600. It writes a temporary file
// and links it into place only when the bot has no file there yet: a crash
// leaves no half-written bot, and a bot is never overwritten.
func (s *Store) Save(m Bot) error {
	path := s.Path(m.Owner, m.Name)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("save the bot: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ") //nolint:gosec // G117: the bot's file is where its key is kept (R9)
	if err != nil {
		return fmt.Errorf("save the bot: %w", err)
	}
	tmp, err := writeTemp(dir, m.Name, data)
	if err != nil {
		return fmt.Errorf("save the bot: %w", err)
	}
	defer func() { _ = os.Remove(tmp) }()
	if err := os.Link(tmp, path); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("save the bot: the bot %s of %s already exists at %s", m.Name, m.Owner, path)
		}
		return fmt.Errorf("save the bot: %w", err)
	}
	return nil
}

// writeTemp writes data to a new file of mode 0600 in dir, named after the
// bot called name, and returns its path.
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
