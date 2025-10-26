package uploader

import (
	"context"
	"errors"
	"testing"
	"time"
	"unicode/utf8"
)

// TestWriteFolderRecord_HookSuccess ensures writeHook is invoked with
// proper id.
func TestWriteFolderRecord_HookSuccess(t *testing.T) {
	var called bool
	var gotCollection, gotID string
	var gotRec FolderRecord

	fs := &Firestore{ctx: context.Background()}
	fs.writeHook = func(col, id string, rec FolderRecord) error {
		called = true
		gotCollection = col
		gotID = id
		gotRec = rec
		return nil
	}

	rec := FolderRecord{
		FolderPath: "a/b",
		UploadedAt: time.Unix(100, 0),
		Files: []UploadedFile{
			{Name: "f.txt", Size: 1},
		},
	}

	if err := fs.WriteFolderRecord("col", rec); err != nil {
		t.Fatalf("WriteFolderRecord: %v", err)
	}
	if !called {
		t.Fatal("hook not called")
	}
	if gotCollection != "col" {
		t.Fatalf("collection mismatch %s", gotCollection)
	}
	wantID := hashPath("a/b")
	if gotID != wantID {
		t.Fatalf("id mismatch got %s want %s", gotID, wantID)
	}
	if gotRec.FolderPath != rec.FolderPath || len(gotRec.Files) != 1 {
		t.Fatal("record mismatch")
	}
}

// TestWriteFolderRecord_HookError ensures errors from hook propagate.
func TestWriteFolderRecord_HookError(t *testing.T) {
	sentinel := errors.New("boom")
	fs := &Firestore{ctx: context.Background()}
	fs.writeHook = func(_, _ string, _ FolderRecord) error {
		return sentinel
	}

	err := fs.WriteFolderRecord("col", FolderRecord{FolderPath: "p"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error got %v", err)
	}
}

// TestWriteFolderRecord_NoCollection ensures empty collection errors.
func TestWriteFolderRecord_NoCollection(t *testing.T) {
	fs := &Firestore{ctx: context.Background()}
	fs.writeHook = func(_, _ string, _ FolderRecord) error {
		t.Fatalf("hook should not be called")
		return nil
	}

	err := fs.WriteFolderRecord("", FolderRecord{FolderPath: "x"})
	if err == nil {
		t.Fatal("expected error for empty collection")
	}
}

// TestWriteFolderRecord_NoClientNoHook verifies defensive error when client
// missing.
func TestWriteFolderRecord_NoClientNoHook(t *testing.T) {
	fs := &Firestore{ctx: context.Background()}
	err := fs.WriteFolderRecord("col", FolderRecord{FolderPath: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestFirestore_CloseNil ensures Close is no-op when nil client.
func TestFirestore_CloseNil(t *testing.T) {
	fs := &Firestore{ctx: context.Background()}
	if err := fs.Close(); err != nil {
		t.Fatalf("close returned error: %v", err)
	}
}

// TestHashPath ensures hashing is deterministic and distinct for different inputs.
func TestHashPath(t *testing.T) {
	inputs := []string{"", "a/b", "a/b/", "a//b", "nested/dir/structure"}
	seen := make(map[string]string)
	allowed := func(r rune) bool {
		if r >= 'a' && r <= 'z' {
			return true
		}
		if r >= 'A' && r <= 'Z' {
			return true
		}
		if r >= '0' && r <= '9' {
			return true
		}
		if r == '-' || r == '_' {
			return true
		}
		return false
	}
	for _, in := range inputs {
		h := hashPath(in)
		if utf8.RuneCountInString(h) != 20 {
			t.Fatalf("hash length = %d want 20 (%q)", utf8.RuneCountInString(h), h)
		}
		for _, r := range h {
			if !allowed(r) {
				t.Fatalf("invalid character %q in hash %q", r, h)
			}
		}
		if prev, exists := seen[h]; exists && prev != in {
			t.Fatalf("hash collision: %q and %q -> %s", prev, in, h)
		}
		// Determinism
		if h2 := hashPath(in); h2 != h {
			t.Fatalf("non-deterministic hash for %q: %s vs %s", in, h, h2)
		}
		seen[h] = in
	}
}
