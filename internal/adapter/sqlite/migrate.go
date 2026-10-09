package sqlite

import (
	"context"
	"database/sql"
	_ "embed" // the migrations' files
	"fmt"
)

//go:embed migrations/0001_processes.sql
var processes string

//go:embed migrations/0002_issues.sql
var issues string

// migrations returns the store's migrations in order: the n-th is the file
// migrations/000n_*.sql, and the store's user_version is n once it is
// applied. A migration only adds, a table or a column with a default, so an
// older crew writes what it knows into a file a newer one migrated.
func migrations() []string {
	return []string{processes, issues}
}

// migrate applies the migrations above the file's user_version and sets it,
// in one immediate transaction, so two processes migrating at once apply
// each migration once. A user_version above the known migrations is a
// newer crew's, and is left as it is.
func migrate(ctx context.Context, db *sql.DB) error {
	if err := inTx(ctx, db, func(tx *sql.Tx) error { return apply(ctx, tx) }); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// apply runs, in tx, the migrations above the user_version, then sets it to
// their number.
func apply(ctx context.Context, tx *sql.Tx) error {
	scripts := migrations()
	var version int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read the store's version: %w", err)
	}
	if version < 0 {
		return fmt.Errorf("the store's version %d is not one crew writes", version)
	}
	if version >= len(scripts) {
		return nil
	}
	for n := version; n < len(scripts); n++ {
		if _, err := tx.ExecContext(ctx, scripts[n]); err != nil {
			return fmt.Errorf("apply the migration %d: %w", n+1, err)
		}
	}
	// PRAGMA takes no bound parameter; the version is a count of migrations.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", len(scripts))); err != nil {
		return fmt.Errorf("set the store's version: %w", err)
	}
	return nil
}
