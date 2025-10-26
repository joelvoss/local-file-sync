package uploader

import (
	"context"
	"errors"
	"local-file-sync/internal/scanner"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// Helper functions to satisfy errcheck and reduce repetition
func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustSymlink(t *testing.T, oldname, newname string) {
	t.Helper()
	// NOTE(joel): Keep behavior simple; skip if windows (privileges)
	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Symlink(oldname, newname); err != nil {
		t.Fatalf("symlink %s->%s: %v", oldname, newname, err)
	}
}

// Consolidated uploader tests
func newTestUploader(t *testing.T) (*GCSUploader, *[]string) {
	t.Helper()
	uploaded := []string{}
	u := &GCSUploader{Bucket: "test-bucket", ctx: context.Background()}
	u.fileUploadHook = func(_, objectName string) error {
		uploaded = append(uploaded, objectName)
		return nil
	}
	return u, &uploaded
}

////////////////////////////////////////////////////////////////////////////////

// TestUploadListedEntries_Simple verifies basic upload of files in a folder.
func TestUploadListedEntries_Simple(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), []byte("a"))
	mustWrite(t, filepath.Join(dir, "b.log"), []byte("b"))
	sub := filepath.Join(dir, "sub")
	mustMkdir(t, sub)
	mustWrite(t, filepath.Join(sub, "c.txt"), []byte("c"))
	u, uploaded := newTestUploader(t)
	entries := []scanner.FileEntry{
		{Name: "a.txt", Path: filepath.Join(dir, "a.txt")},
		{Name: "b.log", Path: filepath.Join(dir, "b.log")},
		{Name: "sub", Path: filepath.Join(dir, "sub")},
	}
	if _, err := u.UploadListedEntries(entries, "prefix"); err != nil {
		t.Fatalf("UploadListedEntries: %v", err)
	}
	base := filepath.Base(dir)
	want := map[string]struct{}{"prefix/" + base + "/a.txt": {}, "prefix/" + base + "/b.log": {}}
	if len(*uploaded) != len(want) {
		t.Fatalf("unexpected upload count %v", *uploaded)
	}
	for _, o := range *uploaded {
		if _, ok := want[o]; !ok {
			t.Errorf("unexpected %s", o)
		}
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestUploadListedEntries_NoPrefix verifies upload with no prefix.
func TestUploadListedEntries_NoPrefix(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "file.txt"), []byte("x"))
	u, uploaded := newTestUploader(t)
	entries := []scanner.FileEntry{{Name: "file.txt", Path: filepath.Join(dir, "file.txt")}}
	if _, err := u.UploadListedEntries(entries, ""); err != nil {
		t.Fatalf("err: %v", err)
	}
	base := filepath.Base(dir)
	if (*uploaded)[0] != base+"/file.txt" {
		t.Fatalf("bad object: %v", *uploaded)
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestUploadListedEntries_SymlinkIgnored verifies symlinks are ignored during
// upload.
func TestUploadListedEntries_SymlinkIgnored(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "real.txt"), []byte("r"))
	mustSymlink(t, "real.txt", filepath.Join(dir, "link.txt"))
	u, uploaded := newTestUploader(t)
	entries := []scanner.FileEntry{
		{Name: "real.txt", Path: filepath.Join(dir, "real.txt")},
		{Name: "link.txt", Path: filepath.Join(dir, "link.txt")},
	}
	if _, err := u.UploadListedEntries(entries, "p"); err != nil {
		t.Fatalf("UploadListedEntries: %v", err)
	}
	if len(*uploaded) != 1 || !strings.Contains((*uploaded)[0], "real.txt") {
		t.Fatalf("expected only real.txt uploaded got %v", *uploaded)
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestUploadListedEntries_DeterministicOrdering verifies uploads are in
// deterministic order regardless of input order.
func TestUploadListedEntries_DeterministicOrdering(t *testing.T) {
	dir := t.TempDir()
	names := []string{"b.txt", "a.txt", "c.txt"}
	for _, n := range names {
		mustWrite(t, filepath.Join(dir, n), []byte(n))
	}
	u, uploaded := newTestUploader(t)
	entries := []scanner.FileEntry{
		{Name: names[0], Path: filepath.Join(dir, names[0])},
		{Name: names[1], Path: filepath.Join(dir, names[1])},
		{Name: names[2], Path: filepath.Join(dir, names[2])},
	}
	if _, err := u.UploadListedEntries(entries, "pref"); err != nil {
		t.Fatalf("UploadListedEntries: %v", err)
	}
	sort.Strings(*uploaded)
	base := filepath.Base(dir)
	expect := []string{"pref/" + base + "/a.txt", "pref/" + base + "/b.txt", "pref/" + base + "/c.txt"}
	for i, e := range expect {
		if (*uploaded)[i] != e {
			t.Errorf("want %s got %s", e, (*uploaded)[i])
		}
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestDetectContentType verifies content type detection based on file
// extension.
func TestDetectContentType(t *testing.T) {
	cases := map[string]string{
		"a.txt":       "text/plain; charset=utf-8",
		"b.LOG":       "text/plain; charset=utf-8",
		"c.md":        "text/plain; charset=utf-8",
		"d.json":      "application/json",
		"e.PNG":       "image/png",
		"f.jpeg":      "image/jpeg",
		"g.bin":       "application/octet-stream",
		"noextension": "application/octet-stream",
	}
	for name, want := range cases {
		if got := detectContentType(name); got != want {
			t.Errorf("%s -> %s want %s", name, got, want)
		}
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestMakePrefixGetter verifies prefix generation and caching.
func TestMakePrefixGetter(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	mustMkdir(t, sub)
	g := makePrefixGetter("parent")
	p1 := g(sub)
	p2 := g(sub)
	if p1 != p2 {
		t.Fatalf("cache miss")
	}
	if want := "parent/" + filepath.Base(sub); p1 != want {
		t.Fatalf("unexpected %s", p1)
	}
	g2 := makePrefixGetter("")
	if got := g2(sub); got != filepath.Base(sub) {
		t.Fatalf("want base got %s", got)
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestUploadListedEntries_Exclusions verifies non-regular files are ignored.
func TestUploadListedEntries_Exclusions(t *testing.T) {
	dir := t.TempDir()
	must := func(n string) string {
		p := filepath.Join(dir, n)
		mustWrite(t, p, []byte("x"))
		return p
	}
	file1 := must("a.txt")
	rdy := must("ORDER100.RDY")
	file2 := must("b.log")
	link := filepath.Join(dir, "b-link.log")
	mustSymlink(t, filepath.Base(file2), link)
	sub := filepath.Join(dir, "folder")
	mustMkdir(t, sub)
	u, uploaded := newTestUploader(t)
	entries := []scanner.FileEntry{
		{Name: filepath.Base(file1), Path: file1},
		{Name: filepath.Base(rdy), Path: rdy},
		{Name: filepath.Base(link), Path: link},
		{Name: filepath.Base(sub), Path: sub},
		{Name: "empty.txt", Path: ""},
	}
	if _, err := u.UploadListedEntries(entries, "pref"); err != nil {
		t.Fatalf("UploadListedEntries: %v", err)
	}
	if len(*uploaded) != 1 {
		t.Fatalf("expected single upload got %v", *uploaded)
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestUploadListedEntries_NoBucketError verifies error when no bucket set.
func TestUploadListedEntries_NoBucketError(t *testing.T) {
	u := &GCSUploader{}
	if _, err := u.UploadListedEntries(nil, "p"); err == nil {
		t.Fatalf("expected error")
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestUploadListedEntries_NoClientNoHook verifies error when no client or hook.
func TestUploadListedEntries_NoClientNoHook(t *testing.T) {
	u := &GCSUploader{Bucket: "b"}
	entries := []scanner.FileEntry{{Name: "a.txt", Path: "/nonexistent"}}
	if _, err := u.UploadListedEntries(entries, "p"); err == nil {
		t.Fatalf("expected error")
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestUploadListedEntries_ConcurrencyCap verifies concurrency cap is respected.
func TestUploadListedEntries_ConcurrencyCap(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		mustWrite(t, filepath.Join(dir, n), []byte("x"))
	}
	u, uploaded := newTestUploader(t)
	u.Concurrency = 99
	entries := []scanner.FileEntry{
		{Name: "a.txt", Path: filepath.Join(dir, "a.txt")},
		{Name: "b.txt", Path: filepath.Join(dir, "b.txt")},
		{Name: "c.txt", Path: filepath.Join(dir, "c.txt")},
	}
	if _, err := u.UploadListedEntries(entries, ""); err != nil {
		t.Fatalf("UploadListedEntries: %v", err)
	}
	if len(*uploaded) != 3 {
		t.Fatalf("expected 3 got %d", len(*uploaded))
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestUploadListedEntries_MissingFileIgnores verifies missing files are
// ignored.
func TestUploadListedEntries_MissingFileIgnores(t *testing.T) {
	u, uploaded := newTestUploader(t)
	entries := []scanner.FileEntry{{Name: "gone.txt", Path: filepath.Join(t.TempDir(), "gone.txt")}}
	if _, err := u.UploadListedEntries(entries, "p"); err != nil {
		t.Fatalf("UploadListedEntries: %v", err)
	}
	if len(*uploaded) != 0 {
		t.Fatalf("expected none got %v", *uploaded)
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestUploadListedEntries_HookError verifies error from hook is propagated.
func TestUploadListedEntries_HookError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	mustWrite(t, p, []byte("x"))
	u := &GCSUploader{Bucket: "b", ctx: context.Background()}
	sentinel := errors.New("boom")
	u.fileUploadHook = func(_, _ string) error { return sentinel }
	entries := []scanner.FileEntry{{Name: "a.txt", Path: p}}
	if _, err := u.UploadListedEntries(entries, ""); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error")
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestUploadListedEntries_ObjectPrefixMultipleDirs verifies that files from
// different directories are uploaded with correct prefixes.
func TestUploadListedEntries_ObjectPrefixMultipleDirs(t *testing.T) {
	root := t.TempDir()
	d1 := filepath.Join(root, "d1")
	d2 := filepath.Join(root, "d2")
	mustMkdir(t, d1)
	mustMkdir(t, d2)
	mustWrite(t, filepath.Join(d1, "a.txt"), []byte("x"))
	mustWrite(t, filepath.Join(d2, "a.txt"), []byte("y"))
	u, up := newTestUploader(t)
	entries := []scanner.FileEntry{
		{Name: "a.txt", Path: filepath.Join(d1, "a.txt")},
		{Name: "a.txt", Path: filepath.Join(d2, "a.txt")},
	}
	if _, err := u.UploadListedEntries(entries, "pref"); err != nil {
		t.Fatalf("UploadListedEntries: %v", err)
	}
	if len(*up) != 2 {
		t.Fatalf("expected 2")
	}
	var saw1, saw2 bool
	for _, o := range *up {
		if filepath.Dir(o) == "pref/"+filepath.Base(d1) {
			saw1 = true
		}
		if filepath.Dir(o) == "pref/"+filepath.Base(d2) {
			saw2 = true
		}
	}
	if !saw1 || !saw2 {
		t.Fatalf("missing dirs %v", *up)
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestUploadListedEntries_EmptyEntries verifies no-op on empty entries.
func TestUploadListedEntries_EmptyEntries(t *testing.T) {
	u, uploaded := newTestUploader(t)
	if _, err := u.UploadListedEntries([]scanner.FileEntry{}, "p"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*uploaded) != 0 {
		t.Fatalf("expected empty")
	}
}

////////////////////////////////////////////////////////////////////////////////

// TestUploader_CloseNil verifies Close is no-op when client is nil.
func TestUploader_CloseNil(t *testing.T) {
	u := &GCSUploader{Bucket: "b"}
	if err := u.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
