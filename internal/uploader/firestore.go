package uploader

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
)

// FolderRecord represents the Firestore document stored per uploaded folder.
type FolderRecord struct {
	FolderPath string         `firestore:"folderPath" json:"folderPath"`
	UploadedAt time.Time      `firestore:"uploadedAt" json:"uploadedAt"`
	Files      []UploadedFile `firestore:"files" json:"files"`
}

// Firestore wraps a firestore client and associated options.
type Firestore struct {
	client *firestore.Client
	ctx    context.Context
	// test hook: optional write bypass for unit tests
	writeHook func(collection, id string, rec FolderRecord) error
}

////////////////////////////////////////////////////////////////////////////////

// NewFirestore creates a new Firestore client using the provided context
// (if nil, Background is used). The supplied context is stored and used as a
// parent for per-operation timeouts. The project ID is detected from the
// environment if possible.
func NewFirestore(ctx context.Context, projectId string) (*Firestore, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := firestore.NewClient(ctx, projectId)
	if err != nil {
		return nil, fmt.Errorf("create firestore client: %w", err)
	}
	return &Firestore{client: client, ctx: ctx}, nil
}

////////////////////////////////////////////////////////////////////////////////

// Close releases the underlying firestore client.
func (f *Firestore) Close() error {
	if f.client != nil {
		return f.client.Close()
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////////

// WriteFolderRecord writes a FolderRecord to the specified collection using
// the folder's base name (or full path hashed if collision-prone) as the
// document ID.
func (f *Firestore) WriteFolderRecord(collection string, rec FolderRecord) error {
	if collection == "" {
		return fmt.Errorf("collection required")
	}
	if f.client == nil && f.writeHook == nil {
		return fmt.Errorf("uploader client not initialized")
	}

	id := hashPath(rec.FolderPath)
	if f.writeHook != nil {
		err := f.writeHook(collection, id, rec)
		return err
	}
	_, err := f.client.Collection(collection).Doc(id).Set(f.ctx, rec)
	return err
}

////////////////////////////////////////////////////////////////////////////////

// hashPath returns a deterministic, short, URL-safe 20 character string derived
// from the first 15 bytes (120 bits) of the SHA-256 hash of the input path,
// encoded with RawURLEncoding (no padding). 120 bits gives 2^120 space;
// birthday bound makes accidental collision astronomically unlikely for any
// realistic number of folders
func hashPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return base64.RawURLEncoding.EncodeToString(sum[:15])
}
