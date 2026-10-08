package crew

import "time"

// Statistic is one record crew keeps in its statistics store, which
// outlives restarts and repositories: Process.
//
//sumtype:decl
type Statistic interface {
	statistic()
}

// ProcessID identifies one crew process in the statistics store, so the
// work it runs can point to it. It is the store's own, apart from the run
// journal's process id.
type ProcessID string

// Process is one crew process, recorded once when it starts. It is not a
// level of the work it runs: one process works on many issues, and one
// issue's runs span many processes.
type Process struct {
	// ID identifies the process.
	ID ProcessID
	// Version is crew's version, as crew --version prints it.
	Version string
	// Folder is the root of the repository the process works in.
	Folder string
	// Start is when the process started.
	Start time.Time
}

func (Process) statistic() {}
