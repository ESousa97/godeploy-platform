package sqlpool

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestForSQLite_nil(t *testing.T) {
	ForSQLite(nil)
}

func TestForSQLite_setsPool(t *testing.T) {
	db, err := sql.Open("sqlite", "file:sqlpooltest?mode=memory")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	ForSQLite(db)
}
