package uploader

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"local-file-sync/internal/app"
	"local-file-sync/internal/scanner"

	"cloud.google.com/go/storage"
)

// GCSUploader uploads local folders (recursively) to a Google Cloud Storage
// bucket. Each file inside the folder is uploaded under an object prefix
// constructed as:
//
//	`<objectPrefix>/<basename(folder)>/<relative path inside folder>`
type GCSUploader struct {
	Bucket      string
	client      *storage.Client
	ctx         context.Context
	Concurrency int
	// test hook: if set, bypass real client
	fileUploadHook func(localPath, objectName string) error
	hookMu         sync.Mutex
}

////////////////////////////////////////////////////////////////////////////////

// NewGCS creates a new uploader using the provided context
// (if nil, Background is used). The supplied context is stored and used as a
// parent for per-file timeouts.
func NewGCS(ctx context.Context, bucket string, concurrency int) (*GCSUploader, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create storage client: %w", err)
	}
	return &GCSUploader{
		Bucket:      bucket,
		client:      client,
		ctx:         ctx,
		Concurrency: concurrency,
	}, nil
}

////////////////////////////////////////////////////////////////////////////////

// Close releases underlying resources.
func (u *GCSUploader) Close() error {
	if u.client != nil {
		return u.client.Close()
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////

type UploadedFile struct {
	Name     string `firestore:"name" json:"name"`
	Size     int64  `firestore:"size" json:"size"`
	Checksum string `firestore:"checksum" json:"checksum"`
	Path     string `firestore:"path" json:"path"`
}

// UploadListedEntries uploads only the specified file entries (non-recursive).
// Directory entries are ignored; only regular files (non-symlink) are uploaded.
func (u *GCSUploader) UploadListedEntries(entries []scanner.FileEntry, objectPrefix string) ([]UploadedFile, error) {
	if u.Bucket == "" {
		return nil, fmt.Errorf("bucket not configured")
	}
	if u.client == nil && u.fileUploadHook == nil {
		return nil, fmt.Errorf("uploader client not initialized")
	}
	if len(entries) == 0 {
		return []UploadedFile{}, nil
	}

	var bucket *storage.BucketHandle
	if u.fileUploadHook == nil {
		bucket = u.client.Bucket(u.Bucket)
	}

	// NOTE(joel): Build a cached prefix getter (avoids repeated string ops
	// per entry).
	getPrefix := makePrefixGetter(objectPrefix)

	var mu sync.Mutex
	meta := make([]UploadedFile, 0, len(entries))
	tasks := make([]app.Task, 0, len(entries))
	for _, fe := range entries {
		name := fe.Name
		localPath := fe.Path
		// NOTE(joel): Guard against empty paths. This should not happen in
		// practice since we control the FileEntry creation, but be defensive.
		if localPath == "" {
			continue
		}

		fi, err := os.Lstat(localPath)
		// NOTE(joel): Skip missing files, symlinks, directories and *.RDY files.
		// We don't want to fail the entire upload in this case.
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() || strings.HasSuffix(strings.ToUpper(name), ".RDY") {
			continue
		}

		// NOTE(joel): Calculate (and cache) prefix per entry.
		dir := filepath.Dir(localPath)
		prefix := getPrefix(dir)

		objectName := prefix + "/" + filepath.ToSlash(name)

		tasks = append(tasks, func(ctx context.Context) error {
			// NOTE(joel): Pre-upload metadata.
			size := fi.Size()
			checksum, err := getChecksum(localPath)
			if err != nil {
				return err
			}

			// NOTE(joel): Perform upload.
			if u.fileUploadHook != nil {
				u.hookMu.Lock()
				err := u.fileUploadHook(localPath, objectName)
				u.hookMu.Unlock()
				return err
			} else {
				if bucket == nil {
					return fmt.Errorf("nil bucket for real upload")
				}
				if err := uploadObject(ctx, bucket, localPath, objectName); err != nil {
					return err
				}
			}

			// NOTE(joel): Record metadata.
			mu.Lock()
			meta = append(meta, UploadedFile{Name: name, Size: size, Checksum: checksum, Path: objectName})
			mu.Unlock()
			return nil
		})
	}
	if len(tasks) == 0 {
		return []UploadedFile{}, nil
	}
	if err := app.RunParallel(u.ctx, u.Concurrency, tasks); err != nil {
		return nil, err
	}
	return meta, nil
}

////////////////////////////////////////////////////////////////////////////////

// uploadObject uploads a single file to GCS as the given object name.
// It uses a per-file timeout derived from the provided context.
func uploadObject(ctx context.Context, bucket *storage.BucketHandle, localPath, objectName string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	obj := bucket.Object(objectName)
	w := obj.NewWriter(ctx)

	w.ContentType = detectContentType(localPath)
	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("copy to gcs %s: %w", objectName, err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finalize object %s: %w", objectName, err)
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////

// makePrefixGetter returns a closure that caches computed object prefixes for
// directories. Given a base objectPrefix (possibly empty) and a directory path
// d, it produces:
//
//	`objectPrefix/<basename(d)>`
//
// or just `<basename(d)>` if objectPrefix is empty. Results are memoized per
// directory string.
func makePrefixGetter(objectPrefix string) func(string) string {
	cache := make(map[string]string, 1)
	return func(dir string) string {
		if p, ok := cache[dir]; ok {
			return p
		}
		base := filepath.Base(dir)
		if objectPrefix != "" {
			p := strings.TrimSuffix(objectPrefix, "/") + "/" + base
			cache[dir] = p
			return p
		}
		cache[dir] = base
		return base
	}
}

////////////////////////////////////////////////////////////////////////////////

// detectContentType is a minimal heuristic; extend as needed.
func detectContentType(path string) string {
	lower := strings.ToLower(filepath.Ext(path))
	switch lower {
	// Text and structured data formats
	case ".csv":
		return "text/csv"
	case ".json":
		return "application/json"
	case ".log", ".md", ".txt":
		return "text/plain; charset=utf-8"
	case ".xml":
		return "application/xml"

	// Image formats (common in scanning workflows)
	case ".bmp":
		return "image/bmp"
	case ".gif":
		return "image/gif"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".svg":
		return "image/svg+xml"
	case ".tif", ".tiff":
		return "image/tiff"
	case ".webp":
		return "image/webp"

	// Document formats
	case ".pdf":
		return "application/pdf"

	// Microsoft Office formats
	case ".doc":
		return "application/msword"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

	// Archive formats
	case ".7z":
		return "application/x-7z-compressed"
	case ".gz":
		return "application/gzip"
	case ".rar":
		return "application/vnd.rar"
	case ".tar":
		return "application/x-tar"
	case ".zip":
		return "application/zip"

	default:
		return "application/octet-stream"
	}
}

////////////////////////////////////////////////////////////////////////////////

// getChecksum computes the SHA256 checksum of the given file and returns it
// as a hex string.
func getChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("checksum open file: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("checksum copy file: %w", err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
