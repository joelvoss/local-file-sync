package app

import (
	"errors"
	"fmt"
	"os"
	"time"
)

// AcquireLock attempts to create a lock file exclusively. It always returns a
// release function that is safe to call even if the lock wasn't acquired. The
// boolean 'acquired' indicates whether this process created (and owns) the
// lock. If the file already exists and is not stale, acquired=false. If it is
// stale (older than the TTL) we attempt a single reclaim.
// `release()` will remove the lock file only if we acquired it. It never panics
// and may be called multiple times idempotently.
func AcquireLock(path string) (release func(), acquired bool, err error) {
	return acquireLockWith(path, 30*time.Minute, time.Now)
}

////////////////////////////////////////////////////////////////////////////////

// acquireLockWith allows tests to inject TTL and clock.
func acquireLockWith(path string, ttl time.Duration, now func() time.Time) (func(), bool, error) {
	owned := false
	// NOTE(joel): We define safe release upfront; closure captures owned flag
	// which will be set true only after successful acquisition. Multiple calls
	// are safe.
	release := func() {
		if !owned {
			return
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Printf("warning: remove lock file %s failed: %v\n", path, err)
		}
		owned = false
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if !errors.Is(err, os.ErrExist) {
			return release, false, fmt.Errorf("create lock file: %w", err)
		}

		// NOTE(joel): File exists; check staleness.
		if info, statErr := os.Stat(path); statErr == nil {
			if now().Sub(info.ModTime()) > ttl {
				// NOTE(joel): Stale; remove and retry once.
				_ = os.Remove(path)
				f2, err2 := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
				if err2 != nil {
					return release, false, nil
				}
				f = f2
			} else {
				return release, false, nil
			}
		} else {
			// NOTE(joel): Can't stat existing file; treat as not acquired
			return release, false, nil
		}
	}

	// NOTE(joel): At this point we have the file handle `f` and own the lock.
	owned = true
	_, _ = fmt.Fprintf(f, "pid=%d time=%s\n", os.Getpid(), now().Format(time.RFC3339Nano))
	if err := f.Close(); err != nil {
		fmt.Printf("warning: close lock file %s failed: %v\n", path, err)
	}
	return release, true, nil
}
