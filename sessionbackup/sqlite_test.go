package sessionbackup

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestPaths(t *testing.T) {
	got, err := Paths("sqlite3", "file:wawebstore.db?foreign_keys=on", "sqlite", "./gobot.sqlite.db")
	if err != nil || got["wawebstore.db"] != "wawebstore.db" {
		t.Fatalf("unexpected SQLite path: %v %v", got, err)
	}
	got, err = Paths("sqlite3", "file:/data/wawebstore.db?foreign_keys=on", "sqlite", "/data/gobot.sqlite.db")
	if err != nil || got["wawebstore.db"] != "/data/wawebstore.db" {
		t.Fatalf("unexpected absolute path: %v %v", got, err)
	}
	if _, err := Paths("postgres", "", "sqlite", "a"); err == nil {
		t.Fatal("non-SQLite backup incorrectly accepted")
	}
}
func TestConsistentSQLiteSnapshot(t *testing.T) {
	dir := t.TempDir()
	wa := dir + "/wawebstore.db"
	db, err := sql.Open("sqlite3", wa)
	if err != nil { t.Fatal(err) }
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE sessions(id INTEGER PRIMARY KEY, token TEXT); INSERT INTO sessions(token) VALUES('secret')"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	snapshot, err := SnapshotSQLite(ctx, wa)
	if err != nil { t.Fatal(err) }
	if !strings.HasPrefix(string(snapshot), "SQLite format 3") { t.Fatal("invalid SQLite snapshot") }
}
