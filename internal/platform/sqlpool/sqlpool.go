// Package sqlpool configures [database/sql] pools for SQLite (e.g. modernc.org/sqlite).
package sqlpool

import (
	"database/sql"
	"time"
)

// ForSQLite applies conservative limits for typical single-file SQLite usage:
// one writer at a time and periodic connection refresh.
func ForSQLite(db *sql.DB) {
	if db == nil {
		return
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
}
