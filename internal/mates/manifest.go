package mates

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
	// Description names the mate and its owner.
	Description string `json:"description"`
	// Public is always false: only the owner can install the app.
	Public bool `json:"public"`
	// Permissions are the repository permissions the app asks for.
	Permissions map[string]string `json:"default_permissions"`
}

// NewManifest returns the manifest of the mate called name for the account
// owner, which sends the browser back to redirectURL. Every mate asks for
// the permissions crew and its sessions use through gh, and none to push
// code or change workflows.
func NewManifest(name, owner, redirectURL string) Manifest {
	return Manifest{
		Name:        AppName(name),
		URL:         homepage,
		RedirectURL: redirectURL,
		Description: "crew mate " + name + " for " + owner + ": an identity of crew, the coding-agent runner.",
		Permissions: map[string]string{
			"issues":        permWrite,
			"pull_requests": permWrite,
			"contents":      permRead,
			"checks":        permRead,
			"statuses":      permRead,
			"actions":       permRead,
			"metadata":      permRead,
		},
	}
}
