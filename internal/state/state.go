package state

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store manages persistent value state for processed RDY files.
type Store struct {
	Path    string
	Data    map[string]int64
	LastRun time.Time
	dirty   bool
	mu      sync.Mutex
}

// diskState defines the structured on-disk representation of state.
type diskState struct {
	Version int              `json:"version"`
	LastRun time.Time        `json:"last_run"`
	Files   map[string]int64 `json:"files"`
}

////////////////////////////////////////////////////////////////////////////////

// New creates a new Store for the given path; data is empty until Load.
func New(path string) *Store {
	return &Store{
		Path: path,
		Data: make(map[string]int64),
	}
}

////////////////////////////////////////////////////////////////////////////////

// Load reads the JSON file if it exists; missing file is not an error.
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Path == "" {
		return nil
	}

	b, err := os.ReadFile(s.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var ds diskState
	if err := json.Unmarshal(b, &ds); err == nil && ds.Files != nil {
		maps.Copy(s.Data, ds.Files)
		s.LastRun = ds.LastRun
		return nil
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////

// Save writes the state atomically; no-op if Path empty.
func (s *Store) Save() error {
	if s.Path == "" || !s.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	ds := diskState{Version: 1, LastRun: s.LastRun, Files: s.Data}
	b, err := json.Marshal(ds)
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		return err
	}
	s.mu.Lock()
	s.dirty = false
	s.mu.Unlock()
	return nil
}

////////////////////////////////////////////////////////////////////////////////

// Get returns stored value and whether it exists.
func (s *Store) Get(path string) (int64, bool) {
	s.mu.Lock()
	v, ok := s.Data[path]
	s.mu.Unlock()
	return v, ok
}

////////////////////////////////////////////////////////////////////////////////

// Set updates the value for a path.
func (s *Store) Set(path string, value int64) {
	s.mu.Lock()
	if cur, ok := s.Data[path]; !ok || cur != value {
		s.Data[path] = value
		s.dirty = true
	}
	s.mu.Unlock()
}

////////////////////////////////////////////////////////////////////////////////

// SetLastRun updates the last run timestamp and marks the store dirty so that
// the persisted state file will reflect the most recent invocation even if no
// new RDY files were discovered.
func (s *Store) SetLastRun(t time.Time) {
	s.mu.Lock()
	s.LastRun = t
	s.dirty = true
	s.mu.Unlock()
}
