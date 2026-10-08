package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

// busyError returns the SQLITE_BUSY a second writer meets, without waiting,
// while another holds the file's write lock.
func busyError(t *testing.T) error {
	t.Helper()
	path := filepath.Join(t.TempDir(), file)
	holder, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Close() })
	lock, err := holder.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Rollback() })
	other, err := sql.Open("sqlite", "file:"+path+"?_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = other.Close() })
	_, err = other.BeginTx(t.Context(), nil)
	if !busy(err) {
		t.Fatalf("second writer's begin = %v, want SQLITE_BUSY", err)
	}
	return fmt.Errorf("begin: %w", err)
}

func TestRetryBusyTriesAgainUntilTheLockIsFree(t *testing.T) {
	errBusy := busyError(t)
	tries := 0

	err := retryBusy(t.Context(), func() error {
		tries++
		if tries < 3 {
			return errBusy
		}
		return nil
	})

	if err != nil || tries != 3 {
		t.Errorf("retryBusy = %v after %d tries, want nil after 3", err, tries)
	}
}

func TestRetryBusyStopsWhenItsContextEnds(t *testing.T) {
	errBusy := busyError(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := retryBusy(ctx, func() error { return errBusy })

	if !busy(err) || !errors.Is(err, context.Canceled) {
		t.Errorf("retryBusy = %v, want SQLITE_BUSY and the context's end", err)
	}
}
