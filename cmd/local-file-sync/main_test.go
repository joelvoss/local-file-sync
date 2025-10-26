package main

import (
	"encoding/json"
	"fmt"
	"io"
	"local-file-sync/internal/app"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// helper to build config for tests.
func testConfig(root, stateFile, lockFile string, stdout *os.File) *app.Config {
	return &app.Config{
		RootDir:        root,
		Recursive:      false,
		FollowSymlinks: false,
		StateFile:      stateFile,
		DisableState:   false,
		LockFile:       lockFile,
		GCSBucket:      "",
		Logger:         log.New(io.Discard, "", 0),
		Stdout:         stdout,
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestRun_EmitsAndState verifies initial emit and subsequent skip leveraging
// state.
func TestRun_EmitsAndState(t *testing.T) {
	root := t.TempDir()
	// NOTE(joel): Prepare RDY + folder structure
	rdy := filepath.Join(root, "ORDER777.RDY")
	if err := os.WriteFile(rdy, []byte("ready"), 0o644); err != nil {
		t.Fatalf("write rdy: %v", err)
	}
	folder := filepath.Join(root, "ORDER777")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatalf("mkdir folder: %v", err)
	}
	if err := os.WriteFile(filepath.Join(folder, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	stateFile := filepath.Join(root, "state.json")
	lockFile := filepath.Join(root, "lockfile")
	outFile, err := os.CreateTemp(root, "out1-*.jsonl")
	if err != nil {
		t.Fatalf("create temp out: %v", err)
	}
	cfg := testConfig(root, stateFile, lockFile, outFile)

	if err := run(cfg); err != nil {
		t.Fatalf("run1: %v", err)
	}
	// NOTE(joel): Rewind and read emitted JSON
	if _, err := outFile.Seek(0, 0); err != nil {
		t.Fatalf("seek: %v", err)
	}
	dec := json.NewDecoder(outFile)
	var matches []map[string]any
	if err := dec.Decode(&matches); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 match got %d", len(matches))
	}

	// NOTE(joel): Capture last modified time of state file then rerun; second
	// run should produce no JSON.
	info1, err := os.Stat(stateFile)
	if err != nil {
		t.Fatalf("stat state: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	outFile2, _ := os.CreateTemp(root, "out2-*.jsonl")
	cfg2 := testConfig(root, stateFile, lockFile, outFile2)
	if err := run(cfg2); err != nil {
		t.Fatalf("run2: %v", err)
	}
	// NOTE(joel): Second output file should be empty (no new matches)
	if fi, _ := outFile2.Stat(); fi.Size() != 0 {
		t.Fatalf("expected no second emit, size=%d", fi.Size())
	}
	// NOTE(joel): State file should have been updated (LastRun changed) => mod
	// time >= original
	info2, err := os.Stat(stateFile)
	if err != nil {
		t.Fatalf("stat2: %v", err)
	}
	if !info2.ModTime().After(info1.ModTime()) && !info2.ModTime().Equal(info1.ModTime()) {
		t.Fatalf("expected state file mod time updated")
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestRun_ReemitOnModTimeChange ensures that when an RDY file's modTime
// changes it is emitted again (re-processing trigger).
func TestRun_ReemitOnModTimeChange(t *testing.T) {
	root := t.TempDir()
	rdy := filepath.Join(root, "ORDER_MOD.RDY")
	if err := os.WriteFile(rdy, []byte("ready"), 0o644); err != nil {
		t.Fatalf("write rdy: %v", err)
	}
	folder := filepath.Join(root, "ORDER_MOD")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatalf("mkdir folder: %v", err)
	}
	stateFile := filepath.Join(root, "state.json")
	lockFile := filepath.Join(root, "lock")
	out1, _ := os.CreateTemp(root, "out-mod1-*.jsonl")
	cfg1 := testConfig(root, stateFile, lockFile, out1)
	if err := run(cfg1); err != nil {
		t.Fatalf("run1: %v", err)
	}
	if _, err := out1.Seek(0, 0); err != nil {
		t.Fatalf("seek1: %v", err)
	}
	dec1 := json.NewDecoder(out1)
	var matches1 []map[string]any
	if err := dec1.Decode(&matches1); err != nil {
		t.Fatalf("decode1: %v", err)
	}
	if len(matches1) != 1 {
		t.Fatalf("expected 1 match got %d", len(matches1))
	}

	// NOTE(joel): Second run with no change -> expect skip.
	out2, _ := os.CreateTemp(root, "out-mod2-*.jsonl")
	cfg2 := testConfig(root, stateFile, lockFile, out2)
	if err := run(cfg2); err != nil {
		t.Fatalf("run2: %v", err)
	}
	if fi, _ := out2.Stat(); fi.Size() != 0 {
		t.Fatalf("expected skip size=%d", fi.Size())
	}

	// NOTE(joel): Touch the RDY file to advance modTime (ensure at least 1ns
	// difference).
	time.Sleep(2 * time.Millisecond)
	now := time.Now()
	if err := os.Chtimes(rdy, now, now); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	out3, _ := os.CreateTemp(root, "out-mod3-*.jsonl")
	cfg3 := testConfig(root, stateFile, lockFile, out3)
	if err := run(cfg3); err != nil {
		t.Fatalf("run3: %v", err)
	}
	if fi, _ := out3.Stat(); fi.Size() == 0 {
		t.Fatalf("expected re-emit after modTime change")
	}
	if _, err := out3.Seek(0, 0); err != nil {
		t.Fatalf("seek3: %v", err)
	}
	dec3 := json.NewDecoder(out3)
	var matches3 []map[string]any
	if err := dec3.Decode(&matches3); err != nil {
		t.Fatalf("decode3: %v", err)
	}
	if len(matches3) != 1 {
		t.Fatalf("expected 1 match after mod change got %d", len(matches3))
	}
	if filepath.Base(matches3[0]["readyFile"].(string)) != "ORDER_MOD.RDY" {
		t.Fatalf("unexpected readyFile")
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestRun_LockNotAcquired ensures graceful exit when lock held.
func TestRun_LockNotAcquired(t *testing.T) {
	root := t.TempDir()
	lockFile := filepath.Join(root, "lock")
	// NOTE(joel): Pre-create lock file to simulate another process
	if err := os.WriteFile(lockFile, []byte("lock"), 0o600); err != nil {
		t.Fatalf("precreate lock: %v", err)
	}
	rdy := filepath.Join(root, "ORDER999.RDY")
	if err := os.WriteFile(rdy, []byte("ready"), 0o644); err != nil {
		t.Fatalf("write rdy: %v", err)
	}
	folder := filepath.Join(root, "ORDER999")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatalf("mkdir folder: %v", err)
	}
	outFile, _ := os.CreateTemp(root, "out-lock-*.jsonl")
	cfg := testConfig(root, filepath.Join(root, "state.json"), lockFile, outFile)
	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	if fi, _ := outFile.Stat(); fi.Size() != 0 {
		t.Fatalf("expected no output when lock not acquired")
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestRun_NoMatches ensures no JSON emitted when there are no *.RDY files.
func TestRun_NoMatches(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "state.json")
	lockFile := filepath.Join(root, "lock")
	outFile, _ := os.CreateTemp(root, "out-nomatch-*.jsonl")
	cfg := testConfig(root, stateFile, lockFile, outFile)
	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	if fi, _ := outFile.Stat(); fi.Size() != 0 {
		t.Fatalf("expected empty output, size=%d", fi.Size())
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestRun_SkipExistingState pre-populates state so one of two matches is
// skipped.
func TestRun_SkipExistingState(t *testing.T) {
	root := t.TempDir()
	rdy1 := filepath.Join(root, "ORDER1.RDY")
	rdy2 := filepath.Join(root, "ORDER2.RDY")
	if err := os.WriteFile(rdy1, []byte("r1"), 0o644); err != nil {
		t.Fatalf("write r1: %v", err)
	}
	if err := os.WriteFile(rdy2, []byte("r2"), 0o644); err != nil {
		t.Fatalf("write r2: %v", err)
	}
	// NOTE(joel): Folders to accompany RDY files
	for _, f := range []string{"ORDER1", "ORDER2"} {
		d := filepath.Join(root, f)
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", f, err)
		}
	}
	stateFile := filepath.Join(root, "state.json")
	// NOTE(joel): Pre-populate state with rdy1 using its actual modTime so it is skipped.
	fi1, err := os.Stat(rdy1)
	if err != nil {
		t.Fatalf("stat rdy1: %v", err)
	}
	content := []byte("{\n  \"version\":1,\n  \"last_run\":\"2025-01-01T00:00:00Z\",\n  \"files\": { \"" + rdy1 + "\": " + fmt.Sprintf("%d", fi1.ModTime().UnixNano()) + " }\n}\n")
	if err := os.WriteFile(stateFile, content, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	lockFile := filepath.Join(root, "lock")
	outFile, _ := os.CreateTemp(root, "out-skip-*.jsonl")
	cfg := testConfig(root, stateFile, lockFile, outFile)
	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	// NOTE(joel): Expect only one emitted match (ORDER2)
	if _, err := outFile.Seek(0, 0); err != nil {
		t.Fatalf("seek: %v", err)
	}
	dec := json.NewDecoder(outFile)
	var matches []map[string]any
	if err := dec.Decode(&matches); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected 1 emitted match got %d", len(matches))
	}
	readyFile, _ := matches[0]["readyFile"].(string)
	if filepath.Base(readyFile) != "ORDER2.RDY" {
		t.Fatalf("expected ORDER2.RDY got %s", readyFile)
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestRun_StateDisabled covers -no-state branch; state file should be ignored
// even if present.
func TestRun_StateDisabled(t *testing.T) {
	root := t.TempDir()
	rdy := filepath.Join(root, "ORDERNOSTATE.RDY")
	if err := os.WriteFile(rdy, []byte("r"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "ORDERNOSTATE"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// NOTE(joel): Pre-create state file to show it's ignored.
	stateFile := filepath.Join(root, "state.json")
	if err := os.WriteFile(stateFile, []byte("{}"), 0o644); err != nil {
		t.Fatalf("state write: %v", err)
	}
	lockFile := filepath.Join(root, "lock")
	outFile, _ := os.CreateTemp(root, "out-nostate-*.jsonl")
	cfg := testConfig(root, stateFile, lockFile, outFile)
	cfg.DisableState = true
	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	if fi, _ := outFile.Stat(); fi.Size() == 0 {
		t.Fatalf("expected emit even with state disabled")
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestRun_StateSaveWarning attempts to provoke a save warning by making
// directory non-writable.
func TestRun_StateSaveWarning(t *testing.T) {
	root := t.TempDir()
	rdy := filepath.Join(root, "ORDERWARN.RDY")
	if err := os.WriteFile(rdy, []byte("r"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "ORDERWARN"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// NOTE(joel): Create a directory with no write permissions for state file.
	badDir := filepath.Join(root, "ro")
	if err := os.Mkdir(badDir, 0o555); err != nil {
		t.Fatalf("mkdir ro: %v", err)
	}
	stateFile := filepath.Join(badDir, "state.json")
	lockFile := filepath.Join(root, "lock")
	outFile, _ := os.CreateTemp(root, "out-warn-*.jsonl")
	cfg := testConfig(root, stateFile, lockFile, outFile)
	if err := run(cfg); err != nil {
		t.Fatalf("run: %v", err)
	}
	// NOTE(joel): Restore perms so cleanup can occur (best effort)
	_ = os.Chmod(badDir, 0o755)
}
