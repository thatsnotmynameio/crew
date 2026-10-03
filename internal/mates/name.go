// Package mates gives crew GitHub identities of its own, called mates. A
// mate is a private GitHub App owned by the account that owns the boss's
// repository: crew creates it through GitHub's manifest flow, keeps its
// private key on the boss's machine, and signs as it to reach GitHub's API.
//
// crew does not act as its mates yet: nothing in the engine sees them.
package mates

import (
	"fmt"
	"strings"
)

// maxName is the longest mate name: crew-<name> then fits the 34 characters
// GitHub allows an app's name.
const maxName = 29

// appPrefix starts the app name crew suggests for a mate.
const appPrefix = "crew-"

// EnvError is an error in crew's environment, found before anything was
// asked of GitHub's pages: an invalid name, no gh, no GitHub repository gh
// resolves, or a saved mate crew cannot read. crew exits 2 on it.
type EnvError struct {
	// Err is what went wrong.
	Err error
}

// Error returns the message of the wrapped error.
func (e *EnvError) Error() string { return e.Err.Error() }

// Unwrap returns the wrapped error.
func (e *EnvError) Unwrap() error { return e.Err }

// envErrorf formats an error, as fmt.Errorf does, and marks it an EnvError.
func envErrorf(format string, args ...any) error {
	return &EnvError{Err: fmt.Errorf(format, args...)}
}

// CheckName returns an EnvError saying which rule name breaks, or nil for a
// valid mate name: 1 to 29 lowercase letters, digits and single hyphens,
// starting and ending with a letter or digit. A valid name is also a safe
// file name.
func CheckName(name string) error {
	switch {
	case name == "":
		return envErrorf("a mate's name cannot be empty")
	case len(name) > maxName:
		return envErrorf("the mate name %q is %d characters long; the limit is %d", name, len(name), maxName)
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return envErrorf("the mate name %q holds %q; a name holds only lowercase letters, digits and hyphens",
				name, r)
		}
	}
	switch {
	case strings.HasPrefix(name, "-"):
		return envErrorf("the mate name %q starts with a hyphen; it must start with a letter or digit", name)
	case strings.HasSuffix(name, "-"):
		return envErrorf("the mate name %q ends with a hyphen; it must end with a letter or digit", name)
	case strings.Contains(name, "--"):
		return envErrorf("the mate name %q holds two hyphens in a row", name)
	}
	return nil
}

// AppName returns the app name crew suggests for the mate name, crew-<name>.
// GitHub's page lets the boss change it.
func AppName(name string) string {
	return appPrefix + name
}
