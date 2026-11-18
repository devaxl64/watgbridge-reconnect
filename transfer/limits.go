// Package transfer limits temporary media storage consumption by active uploads.
package transfer

import (
	"errors"
	"sync"
)

const (
	// MediaBudget is the maximum total space reserved for in-flight media
	// files (600 MiB). The container also requires disk space for session
	// state, OS files and other temporary data.
	MediaBudget int64 = 600 * 1024 * 1024
)

var (
	ErrUnknownSize = errors.New("media size must be known and positive before download")
	ErrFileTooLarge = errors.New("file exceeds 600 MiB temporary media limit")
	ErrBusy = errors.New("temporary media limit occupied by other downloads; try again when transfers complete")
)

// Limiter accounts for disk space reserved by simultaneous media transfers.
// It does not check actual free disk space: callers must verify available
// ephemeral storage before writing.
type Limiter struct {
	mu sync.Mutex
	used int64
}

// Reserve allocates the declared file size and returns an idempotent release
// function. The caller MUST invoke release when the transfer completes, fails,
// or is canceled. It must also delete any temporary file.
func (l *Limiter) Reserve(size int64) (release func(), err error) {
	if size <= 0 {
		return nil, ErrUnknownSize
	}
	if size > MediaBudget {
		return nil, ErrFileTooLarge
	}

	l.mu.Lock()
	if size > MediaBudget-l.used {
		l.mu.Unlock()
		return nil, ErrBusy
	}
	l.used += size
	l.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.used -= size
			l.mu.Unlock()
		})
	}, nil
}

func (l *Limiter) Used() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.used
}

func (l *Limiter) Available() int64 {
	return MediaBudget-l.Used()
}
