package state

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestStore_LoadSave verifies saving and loading state to/from JSON file.
func TestStore_LoadSave(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	s := New(p)
	s.Set("/tmp/file1.RDY", 123)
	s.Set("/tmp/file2.RDY", 456)
	now := time.Now().UTC().Truncate(time.Second)
	s.LastRun = now
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// NOTE(joel): Load into new store
	s2 := New(p)
	if err := s2.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if v, ok := s2.Get("/tmp/file1.RDY"); !ok || v != 123 {
		t.Fatalf("bad value1: %v %v", v, ok)
	}
	if v, ok := s2.Get("/tmp/file2.RDY"); !ok || v != 456 {
		t.Fatalf("bad value2: %v %v", v, ok)
	}

	if s2.LastRun.IsZero() {
		t.Fatalf("expected LastRun restored")
	}
	// NOTE(joel): Overwrite one value and save again.
	s2.Set("/tmp/file1.RDY", 789)
	s2.LastRun = now.Add(time.Minute)
	if err := s2.Save(); err != nil {
		t.Fatalf("resave: %v", err)
	}

	// NOTE(joel): Inspect file size to ensure content written.
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("expected non-empty state file")
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestStore_DirtyBehavior verifies the dirty flag behavior of the store.
func TestStore_DirtyBehavior(t *testing.T) {
	s := New("")
	s.Set("a.RDY", 1)
	if !s.dirty {
		t.Fatalf("expected dirty after first set")
	}
	s.dirty = false
	s.Set("a.RDY", 1)
	if s.dirty {
		t.Fatalf("expected not dirty after idempotent set")
	}
	s.SetLastRun(time.Now())
	if !s.dirty {
		t.Fatalf("expected dirty after SetLastRun")
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestStore_LoadInvalidJSON verifies that loading invalid JSON does not error
// and results in empty state.
func TestStore_LoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	if err := os.WriteFile(p, []byte("not-json"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := New(p)
	if err := s.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(s.Data) != 0 {
		t.Fatalf("expected empty data on invalid JSON")
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestStore_LoadMissing verifies that loading a missing file does not error.
func TestStore_LoadMissing(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "missing.json"))
	if err := s.Load(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestStore_SaveNoPathNoDirty verifies that saving with no path or not dirty
// is a no-op.
func TestStore_SaveNoPathNoDirty(t *testing.T) {
	s := New("")
	if err := s.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	s.Path = filepath.Join(t.TempDir(), "state.json")
	if err := s.Save(); err != nil {
		t.Fatalf("save2: %v", err)
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestStore_ConcurrentSet ensures Set can be safely called concurrently.
func TestStore_ConcurrentSet(t *testing.T) {
	s := New("")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := filepath.Join("/tmp", "file", "f", "f")
			s.Set(key, int64(idx))
		}(i)
	}
	wg.Wait()
	// No assertion on exact value (last write wins) but map access must succeed
	if _, ok := s.Get(filepath.Join("/tmp", "file", "f", "f")); !ok {
		t.Fatalf("expected at least one value written")
	}
}
