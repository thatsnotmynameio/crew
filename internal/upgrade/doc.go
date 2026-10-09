// Package upgrade holds what crew upgrade decides and does: the release
// it is asked for, whether it may replace the running crew at all, the
// client that finds that release on GitHub, and the install of the crew
// that release holds.
//
// ParseArgs reads crew upgrade's one optional argument, a release version
// written vX.Y.Z or X.Y.Z, and returns it as vX.Y.Z, or an EnvError, the
// error on which crew exits 2. Classify tells a release build, the only
// kind crew upgrade replaces, from a go install build and a local one.
//
// Client finds a release of thatsnotmynameio/crew, the latest or one by
// tag, through GitHub's REST API, and downloads its assets by id, each
// under a cap. It sends the token of the user's gh login, from GHToken,
// when there is one, so it reaches the releases of a private repository
// as well as a public one, and goes anonymous otherwise. Its errors name
// their cause; a cancelled context is ErrInterrupted.
//
// ResolveAPI returns GitHub's API, or the one CREW_UPGRADE_API_URL (APIEnv)
// names: an http or https URL of a loopback IP address with at most a port,
// for the acceptance suite's fake GitHub. Any other value is an EnvError.
// Under the override, gh is asked for the override's host with its token
// variables unset, so no github.com token reaches a local server, and
// API.Notice is the line crew prints to say the override is in use.
//
// Client.Binary downloads a release's archive for the machine and its
// checksums.txt, checks the archive's SHA-256 against its one line there
// before it opens it, and returns the one regular crew the archive holds.
// Target finds the running crew's path with its symlinks resolved, and
// CheckWritable tells whether crew may replace it. Replace writes the new
// crew to a hidden temp file beside it and renames that over it, so a
// failure changes nothing and a running crew keeps its old binary. A path
// crew may not write is an EnvError that points to the README's install
// into ~/.local/bin: crew upgrade never uses sudo.
//
// The package imports only the standard library and internal/proc, and
// only cmd/crew imports it: the upgrade involves no tracker, engine or
// port. The upgrade and upgrade-users rules of depguard hold both.
package upgrade
