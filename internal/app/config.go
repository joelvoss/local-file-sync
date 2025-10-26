package app

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Config centralizes all runtime options for local-file-sync.
type Config struct {
	RootDir             string
	Recursive           bool
	FollowSymlinks      bool
	StateFile           string
	DisableState        bool
	LockFile            string
	GCSBucket           string
	FirestoreProjectId  string
	FirestoreCollection string
	FolderConcurrency   int
	FileConcurrency     int
	Logger              *log.Logger
	Stdout              *os.File
}

////////////////////////////////////////////////////////////////////////////////

// ParseFlags defines and parses command-line flags into a Config.
func ParseFlags() (*Config, error) {
	var (
		dir          string
		recursive    bool
		followLinks  bool
		stateFile    string
		disableState bool
		lockFile     string
		gcsBucket    string
		fsString     string
		folderConc   int
		fileConc     int
	)
	flag.StringVar(&dir, "dir", ".", "Directory to scan")
	flag.BoolVar(&recursive, "recursive", false, "Recursively scan for *.RDY files")
	flag.BoolVar(&followLinks, "follow-symlinks", false, "Follow directory symlinks when recursive")
	flag.StringVar(&stateFile, "state-file", "", "Path to persistent state file (default: <dir>/.local-file-sync_state.json)")
	flag.BoolVar(&disableState, "no-state", false, "Disable state persistence entirely (no reading or writing state file)")
	flag.StringVar(&lockFile, "lock-file", "", "Path to lock file (default: per-directory hash in /tmp)")
	flag.StringVar(&gcsBucket, "gcs-bucket", "", "If set, upload each newly emitted matched folder's files to the given GCS bucket (requires GOOGLE_APPLICATION_CREDENTIALS or ADC)")
	flag.StringVar(&fsString, "firestore", "", "If set, write a Firestore document per successfully uploaded folder in the format PROJECT_ID:COLLECTION (requires -gcs-bucket)")
	flag.IntVar(&folderConc, "folder-concurrency", 0, "Max concurrent folder uploads (0=auto)")
	flag.IntVar(&fileConc, "file-concurrency", 0, "Max concurrent file uploads within a folder (0=auto)")
	flag.Parse()

	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve dir: %w", err)
	}

	if fsString != "" && gcsBucket == "" {
		return nil, fmt.Errorf("-firestore requires -gcs-bucket")
	}

	// NOTE(joel): Parse the firestore string if provided.
	// Expected format: PROJECT_ID:COLLECTION
	var fsProjectId, fsCollection string
	if fsString != "" {
		var ok bool
		fsProjectId, fsCollection, ok = strings.Cut(fsString, ":")
		if !ok || fsProjectId == "" || fsCollection == "" {
			return nil, fmt.Errorf("invalid -firestore format, expected PROJECT_ID:COLLECTION")
		}
	}

	cfg := &Config{
		RootDir:             abs,
		Recursive:           recursive,
		FollowSymlinks:      followLinks,
		StateFile:           stateFile,
		DisableState:        disableState,
		LockFile:            lockFile,
		GCSBucket:           gcsBucket,
		FirestoreProjectId:  fsProjectId,
		FirestoreCollection: fsCollection,
		FolderConcurrency:   folderConc,
		FileConcurrency:     fileConc,
		Logger:              log.New(os.Stderr, "", log.LstdFlags),
		Stdout:              os.Stdout,
	}

	// NOTE(joel): Derive defaults for state and lock files if not explicitly set.
	if cfg.LockFile == "" {
		h := sha256.Sum256([]byte(cfg.RootDir))
		short := hex.EncodeToString(h[:8])
		cfg.LockFile = filepath.Join(os.TempDir(), fmt.Sprintf("local-file-sync-%s.lock", short))
	}
	if cfg.StateFile == "" {
		cfg.StateFile = filepath.Join(cfg.RootDir, ".local-file-sync_state.json")
	}
	return cfg, nil
}
