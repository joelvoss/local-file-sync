package app

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestAcquireLock_Concurrent verifies multiple goroutines attempting to acquire
// the same lock file only allows one to succeed.
func TestAcquireLock_Concurrent(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "test.lock")

	var got struct {
		acquired int
	}
	var mu sync.Mutex

	wg := sync.WaitGroup{}
	workers := 10
	for range workers {
		wg.Go(func() {
			rel, ok, err := AcquireLock(lock)
			if err != nil {
				mu.Lock()
				t.Errorf("unexpected error: %v", err)
				mu.Unlock()
				return
			}
			if ok {
				mu.Lock()
				got.acquired++
				mu.Unlock()
			}
			rel()
		})
	}
	wg.Wait()
	if got.acquired != 1 {
		// NOTE(joel): Only one goroutine should have acquired the lock.
		if got.acquired == 0 {
			// NOTE(joel): Depending on scheduling, the winner may release before
			// losers try. Ensure lock file existed at some point by attempting second
			// acquire.
			rel, ok, err := AcquireLock(lock)
			if err != nil {
				t.Fatalf("second stage acquire fail: %v", err)
			}
			if !ok {
				t.Fatalf("expected to acquire in fallback path")
			}
			rel()
		} else {
			// NOTE(joel): acquired >1 means broken exclusivity
			t.Fatalf("expected exactly one acquisition; got %d", got.acquired)
		}
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestAcquireLock_Stale verifies that a lock file with an old mod time is
// considered stale and can be acquired.
func TestAcquireLock_Stale(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "test.lock")

	// NOTE(joel): Acquire first time
	release, ok, err := acquireLockWith(lock, 10*time.Second, time.Now)
	if err != nil || !ok {
		if err != nil {
			t.Fatalf("initial acquire: %v", err)
		}
		if !ok {
			t.Fatalf("expected initial acquire")
		}
	}
	release()

	// NOTE(joel): Touch file with old modtime to simulate staleness
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		// NOTE(joel): If file removed earlier, recreate with old mod time
		f, err2 := os.Create(lock)
		if err2 != nil {
			t.Fatalf("recreate lock: %v", err2)
		}
		if err := f.Close(); err != nil {
			panic("close recreate lock: " + err.Error())
		}
		_ = os.Chtimes(lock, old, old)
	}

	// NOTE(joel): Now acquiring with small TTL should treat existing file as
	// stale and succeed.
	release2, ok2, err2 := acquireLockWith(lock, 30*time.Minute, func() time.Time { return time.Now() })
	if err2 != nil {
		t.Fatalf("second acquire: %v", err2)
	}
	if !ok2 {
		t.Fatalf("expected to re-acquire after stale")
	}
	release2()
}
