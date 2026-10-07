package bots

// The access levels a manifest's permissions grant.
const (
	permRead  = "read"
	permWrite = "write"
)

// homepage is the app's homepage, the one key a manifest requires.
const homepage = "https://github.com/thatsnotmynameio/crew"

// Manifest is the GitHub App manifest crew posts to GitHub's create page.
// It has no hook_attributes and no default_events, so the app gets no
// webhook, and it is private.
type Manifest struct {
	// Name is the app name GitHub's page suggests.
	Name string `json:"name"`
	// URL is the app's homepage.
	URL string `json:"url"`
	// RedirectURL is where GitHub sends the browser, with the code to
	// convert, once the app exists.
	RedirectURL string `json:"redirect_url"`
	// Description names the bot and its owner.
	Description string `json:"description"`
	// Public is always false: only the owner can install the app.
	Public bool `json:"public"`
	// Permissions are the repository permissions the app asks for.
	Permissions map[string]string `json:"default_permissions"`
}

// NewManifest returns the manifest of the bot called name for the account
// owner, which sends the browser back to redirectURL. Every bot asks for
// the same permissions.
func NewManifest(name, owner, redirectURL string) Manifest {
	return Manifest{
		Name:        AppName(name),
		URL:         homepage,
		RedirectURL: redirectURL,
		Description: "crew bot " + name + " for " + owner + ": an identity of crew, the coding-agent runner.",
		Permissions: permissions(),
	}
}

// permissions returns the repository permissions every bot asks for, in
// its manifest and in each token crew mints: those crew and its sessions
// use through gh, and none to change workflows. contents is write only
// because GitHub refuses to resolve a review thread without it: pushes
// keep your own git credentials (gitenv.go). Each call returns a new map.
func permissions() map[string]string {
	return map[string]string{
		"issues":        permWrite,
		"pull_requests": permWrite,
		"contents":      permWrite,
		"checks":        permRead,
		"statuses":      permRead,
		"actions":       permRead,
		"metadata":      permRead,
	}
}
