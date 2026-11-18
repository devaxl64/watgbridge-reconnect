package sessionbackup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

// Paths works with the two SQLite databases shipped by the original bridge.
// It explicitly refuses non-SQLite databases rather than promising to back
// them up using a filesystem snapshot.
func Paths(waDriver, waURL, bridgeDriver, bridgePath string) (map[string]string, error) {
	if waDriver != "sqlite3" || bridgeDriver != "sqlite" {
		return nil, errors.New("encrypted session backup currently supports only SQLite")
	}
	if waURL == "" || bridgePath == "" || strings.Contains(waURL, "mode=memory") {
		return nil, errors.New("invalid SQLite backup paths")
	}
	waPath := waURL
	if strings.HasPrefix(waURL, "file:") {
		parsed, err := url.Parse(waURL)
		if err != nil {
			return nil, err
		}
		if parsed.Opaque != "" {
			waPath = parsed.Opaque
		} else {
			waPath = parsed.Path
		}
	} else if i := strings.IndexByte(waPath, '?'); i >= 0 {
		waPath = waPath[:i]
	}
	if waPath == "" || waPath == ":memory:" || bridgePath == ":memory:" {
		return nil, errors.New("cannot back up in-memory SQLite databases")
	}
	return map[string]string{
		"wawebstore.db": waPath,
		"gobot.sqlite.db": bridgePath,
	}, nil
}

// SnapshotSQLite takes a transactionally consistent copy with VACUUM INTO.
// Never copy a live SQLite file via a plain os.ReadFile: it can depend on WAL.
func SnapshotSQLite(ctx context.Context, path string) ([]byte, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("SQLite database unavailable for backup: %w", err)
	}
	tempDir, err := os.MkdirTemp("", "watg-sqlite-snapshot-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	tempPath := filepath.Join(tempDir, "snapshot.db")
	// Use URI mode=rw so a missing source database is never created accidentally.
	source := "file:" + path + "?mode=rw&_busy_timeout=10000"
	db, err := sql.Open("sqlite3", source)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", tempPath); err != nil {
		return nil, fmt.Errorf("consistent SQLite snapshot failed: %w", err)
	}
	info, err := os.Stat(tempPath)
	if err != nil {
		return nil, err
	}
	if info.Size() <= 0 || info.Size() > MaxPlainFileSize {
		return nil, fmt.Errorf("SQLite snapshot exceeds size limit (%d bytes)", info.Size())
	}
	return os.ReadFile(tempPath)
}

// SnapshotBoth creates independent consistent snapshots before encrypting.
// These databases are owned by separate SQLite connections in WaTgBridge.
func SnapshotBoth(ctx context.Context, paths map[string]string) (map[string][]byte, error) {
	files := make(map[string][]byte, len(expectedFiles))
	for _, name := range expectedFiles {
		path := paths[name]
		if path == "" {
			return nil, fmt.Errorf("missing path for %s", name)
		}
		data, err := SnapshotSQLite(ctx, path)
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
	return files, nil
}
