// Package fileline appends lines to files that crew only ever appends to,
// such as the run journal and the session logs. It imports only the
// standard library.
package fileline

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// The permissions of the files Open creates and of their directories: logs
// hold what sessions printed, so only you read them.
const (
	dirPerm  = 0o700
	filePerm = 0o600
)

// Open opens the file at path for reading and appending, creating it and
// its directory as needed.
func Open(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, fmt.Errorf("create the log directory: %w", err)
	}
	//nolint:gosec // crew builds the path under .crew/logs
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, filePerm)
	if err != nil {
		return nil, fmt.Errorf("open for appending: %w", err)
	}
	return f, nil
}

// Append appends data to f, a file opened for reading and appending, as a
// line of its own: when f ends in the middle of a line, as after a crash
// during a write, it ends that line first.
func Append(f *os.File, data []byte) error {
	if err := startLine(f); err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append the line: %w", err)
	}
	return nil
}

// startLine makes the next write to f, a file opened for reading and
// appending, start on a line of its own: when f ends in the middle of a
// line, it writes a newline first.
func startLine(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("find the end of the last line: %w", err)
	}
	if info.Size() == 0 {
		return nil
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("find the end of the last line: %w", err)
	}
	if last[0] == '\n' {
		return nil
	}
	if _, err := f.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("end the last line: %w", err)
	}
	return nil
}
