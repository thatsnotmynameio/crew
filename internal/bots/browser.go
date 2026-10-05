package bots

import (
	"runtime"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// OpenBrowser opens url in your browser with open on macOS, or
// xdg-open on Linux, started detached: the browser outlives crew, and crew
// does not wait for it. Only a failure to start the opener is an error.
func OpenBrowser(url string) error {
	return proc.StartDetached(proc.Command{Name: browserCommand(runtime.GOOS), Args: []string{url}})
}

// browserCommand returns the program that opens a URL on the system goos.
func browserCommand(goos string) string {
	if goos == "darwin" {
		return "open"
	}
	return "xdg-open"
}
