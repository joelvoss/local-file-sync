package scanner

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestScan verifies basic scanning behavior.
func TestScan(t *testing.T) {
	dir := t.TempDir()

	// NOTE(joel): Create sample .RDY files and folders.
	rdy1 := filepath.Join(dir, "ORDER123.RDY")
	if err := os.WriteFile(rdy1, []byte("ready"), 0o644); err != nil {
		t.Fatalf("write rdy1: %v", err)
	}
	folder1 := filepath.Join(dir, "ORDER123")
	if err := os.Mkdir(folder1, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(folder1, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write nested: %v", err)
	}

	// NOTE(joel): .RDY without folder.
	rdy2 := filepath.Join(dir, "MISSING.RDY")
	if err := os.WriteFile(rdy2, []byte("ready"), 0o644); err != nil {
		t.Fatalf("write rdy2: %v", err)
	}

	matches, err := Scan(dir, Options{})
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	// NOTE(joel): Verify one has folder and one missing.
	var foundFolder, foundMissing bool
	for _, m := range matches {
		switch filepath.Base(m.ReadyFile) {
		case "ORDER123.RDY":
			if m.MissingFolder {
				t.Errorf("expected folder present for ORDER123")
			}
			if len(m.FolderEntries) != 1 {
				t.Errorf("expected 1 entry in folder, got %d", len(m.FolderEntries))
			}
			foundFolder = true
		case "MISSING.RDY":
			if !m.MissingFolder {
				t.Errorf("expected missing folder for MISSING")
			}
			foundMissing = true
		}
	}
	if !foundFolder || !foundMissing {
		t.Errorf("did not find expected matches; got %+v", matches)
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestScan_RecursiveSymlinks verifies recursive scanning and symlink behavior.
func TestScan_RecursiveSymlinks(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "inner")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatalf("mkdir inner: %v", err)
	}
	rdy := filepath.Join(inner, "ORDER1.RDY")
	if err := os.WriteFile(rdy, []byte("rdy"), 0o644); err != nil {
		t.Fatalf("write rdy: %v", err)
	}
	folder := filepath.Join(inner, "ORDER1")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatalf("mkdir order1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(folder, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	link := filepath.Join(root, "linkInner")
	if runtime.GOOS != "windows" {
		if err := os.Symlink("inner", link); err != nil {
			t.Fatalf("symlink: %v", err)
		}
	}
	m1, err := Scan(root, Options{Recursive: true, FollowSymlinks: false})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(m1) != 1 {
		t.Fatalf("expected 1 match got %d", len(m1))
	}
	m2, err := Scan(root, Options{Recursive: true, FollowSymlinks: true})
	if err != nil {
		t.Fatalf("scan2: %v", err)
	}
	if len(m2) != 1 {
		t.Fatalf("expected 1 match got %d (follow symlinks)", len(m2))
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestScan_NonDirRoot verifies error when root is not a directory.
func TestScan_NonDirRoot(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Scan(f, Options{}); err == nil {
		t.Fatalf("expected error for non-directory root")
	}
}
