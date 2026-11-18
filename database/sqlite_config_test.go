package database

import (
	"path/filepath"
	"testing"

	"gorm.io/gorm"
)

func TestOpenSQLiteDatabaseSerializesWritersAndEnablesWAL(t *testing.T) {
	db, err := openSQLiteDatabase(filepath.Join(t.TempDir(), "bridge.db"), &gorm.Config{})
	if err != nil { t.Fatal(err) }
	pool, err := db.DB()
	if err != nil { t.Fatal(err) }
	defer pool.Close()
	if got := pool.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("expected one SQLite connection, got %d", got)
	}
	var mode string
	if err := db.Raw("PRAGMA journal_mode").Scan(&mode).Error; err != nil {
		t.Fatal(err)
	}
	if mode != "wal" { t.Fatalf("WAL journal required, got %q", mode) }
}
