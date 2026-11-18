package transfer

import (
	"errors"
	"sync"
	"testing"
)

func TestRejectLargerThan600MiB(t *testing.T) {
	var limiter Limiter
	if _, err := limiter.Reserve(MediaBudget+1); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("expected ErrFileTooLarge, got %v", err)
	}
	if limiter.Used() != 0 {
		t.Fatal("rejected transfer reserved space")
	}
}

func TestConcurrentTransferQuotaAndRelease(t *testing.T) {
	var limiter Limiter
	first, err := limiter.Reserve(350*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	second, err := limiter.Reserve(250*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limiter.Reserve(1); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy when capacity full, got %v", err)
	}
	if limiter.Available() != 0 {
		t.Fatal("expected zero available capacity")
	}
	first()
	first() // release is idempotent
	if limiter.Used() != 250*1024*1024 {
		t.Fatal("double release changed reserved space")
	}
	second()
	if limiter.Used() != 0 {
		t.Fatal("all reservations released but budget remains occupied")
	}
}

func TestInvalidSizes(t *testing.T) {
	var limiter Limiter
	for _, size := range []int64{0, -1} {
		if _, err := limiter.Reserve(size); !errors.Is(err, ErrUnknownSize) {
			t.Fatalf("size %d: expected ErrUnknownSize, got %v", size, err)
		}
	}
}

func TestConcurrentReservationsDoNotExceedBudget(t *testing.T) {
	var limiter Limiter
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			release, err := limiter.Reserve(100 * 1024 * 1024)
			if err == nil {
				defer release()
				if limiter.Used() > MediaBudget {
					t.Error("budget overallocated")
				}
			} else if !errors.Is(err, ErrBusy) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if limiter.Used() != 0 {
		t.Fatalf("leaked reservations: %d", limiter.Used())
	}
}
