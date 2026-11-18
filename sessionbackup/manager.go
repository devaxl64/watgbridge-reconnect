package sessionbackup

import (
	"context"
	"errors"
	"os"
	"time"
)

type Manager struct {
	key []byte
	paths map[string]string
	store *TelegramStore
}

// NewManagerFromEnv is deliberately opt-in: a missing WATG_BACKUP_KEY does not
// change existing deployments. The key must be generated and stored in
// Northflank runtime secrets, never committed to the GitHub repository.
func NewManagerFromEnv(token string, ownerID int64, waType, waURL, dbType, dbPath string) (*Manager, error) {
	if os.Getenv("WATG_BACKUP_KEY") == "" {
		return nil, nil
	}
	key, err := KeyFromEnvironment()
	if err != nil {
		return nil, err
	}
	paths, err := Paths(waType, waURL, dbType, dbPath)
	if err != nil {
		return nil, err
	}
	store, err := NewTelegramStore(token, ownerID)
	if err != nil {
		return nil, err
	}
	return &Manager{key: key, paths: paths, store: store}, nil
}

// RestoreIfMissing runs before any database clients connect. Even if just
// one local SQLite database is missing, the encrypted archive restores it
// without overwriting any existing database.
func (m *Manager) RestoreIfMissing(ctx context.Context) (bool, error) {
	if m == nil {
		return false, nil
	}
	missing := false
	for _, path := range m.paths {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			missing = true
		} else if err != nil {
			return false, err
		}
	}
	if !missing {
		return false, nil
	}
	blob, err := m.store.Download(ctx)
	if errors.Is(err, ErrNoBackup) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	files, err := Open(m.key, blob)
	if err != nil {
		return false, err
	}
	count, err := RestoreMissing(files, m.paths)
	return count > 0, err
}

// Save is safe to run while SQLite databases are live. Snapshots are generated
// with SQLite VACUUM INTO before being compressed, authenticated and uploaded.
func (m *Manager) Save(ctx context.Context) error {
	if m == nil {
		return nil
	}
	files, err := SnapshotBoth(ctx, m.paths)
	if err != nil {
		return err
	}
	blob, err := Seal(m.key, files)
	if err != nil {
		return err
	}
	return m.store.Upload(ctx, blob)
}

// RunPeriodicBackup keeps a single backup worker active, without storing
// copies indefinitely on the Northflank container. Call with a live WA session.
func (m *Manager) RunPeriodicBackup(ctx context.Context, interval time.Duration, shouldBackup func() bool, report func(error)) {
	if m == nil {
		return
	}
	if interval < 5*time.Minute {
		interval = 30 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		var nextAttempt time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if shouldBackup != nil && !shouldBackup() {
					continue
				}
				if time.Now().Before(nextAttempt) {
					continue
				}
				opCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
				err := m.Save(opCtx)
				cancel()
				if err != nil {
					if report != nil {
						report(err)
					}
					// Retrying every 30 seconds during Telegram outages would
					// cause unnecessary load and repeated error messages.
					nextAttempt = time.Now().Add(5 * time.Minute)
				} else {
					// Upload promptly after the first successful pairing,
					// then update the private pinned backup periodically.
					nextAttempt = time.Now().Add(interval)
				}
			}
		}
	}()
}
