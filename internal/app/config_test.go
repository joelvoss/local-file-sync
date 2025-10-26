package app

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resetFlags resets the default flag.CommandLine for tests that re-use
// ParseFlags.
func resetFlags() {
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
}

////////////////////////////////////////////////////////////////////////////////

// TestParseFlags_Defaults verifies defaults are set as expected.
func TestParseFlags_Defaults(t *testing.T) {
	resetFlags()
	dir := t.TempDir()
	os.Args = []string{"cmd", "-dir", dir}
	cfg, err := ParseFlags()
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.RootDir != filepath.Clean(dir) {
		t.Fatalf("root mismatch: %s vs %s", cfg.RootDir, dir)
	}
	if cfg.StateFile == "" || !strings.Contains(cfg.StateFile, ".local-file-sync_state.json") {
		t.Fatalf("unexpected default state file: %s", cfg.StateFile)
	}
	if cfg.LockFile == "" || !strings.Contains(cfg.LockFile, "local-file-sync-") {
		t.Fatalf("unexpected lock file: %s", cfg.LockFile)
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestParseFlags_Overrides verifies flags override defaults as expected.
func TestParseFlags_Overrides(t *testing.T) {
	resetFlags()
	dir := t.TempDir()
	sf := filepath.Join(dir, "custom.json")
	lf := filepath.Join(dir, "custom.lock")
	os.Args = []string{"cmd", "-dir", dir, "-state-file", sf, "-lock-file", lf, "-recursive", "-follow-symlinks", "-no-state", "-gcs-bucket", "b"}
	cfg, err := ParseFlags()
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.StateFile != sf || cfg.LockFile != lf || !cfg.Recursive || !cfg.FollowSymlinks || !cfg.DisableState || cfg.GCSBucket != "b" {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}
