package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"local-file-sync/internal/app"
	"local-file-sync/internal/scanner"
	"local-file-sync/internal/state"
	"local-file-sync/internal/uploader"
)

// NOTE(joel): version is overridden at build time via -ldflags "-X main.
// version=<value>". See `./Taskfile.sh build` task). Defaults to "dev" when
// running via `go run`.
var version = "dev"

// Main is the entry point for the local-file-sync command-line tool.
func main() {
	cfg, err := app.ParseFlags()
	if err != nil {
		cfg.Logger.Fatalf("error: %v\n", err)
	}
	cfg.Logger.Printf("local-file-sync version=%s", version)
	if err := run(cfg); err != nil {
		cfg.Logger.Fatalf("fatal: %v\n", err)
	}
}

////////////////////////////////////////////////////////////////////////////////

// run executes the main logic based on the provided configuration.
func run(cfg *app.Config) error {
	// NOTE(joel): Acquire a process-level lock to avoid two concurrent
	// local-file-sync processes handling the same *.RDY files simultaneously.
	lockPath := cfg.LockFile
	release, acquired, err := app.AcquireLock(lockPath)
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	// NOTE(joel): release is a no-op if not acquired
	defer release()

	if !acquired {
		cfg.Logger.Printf("another local-file-sync process holds lock %s; skip execution", lockPath)
		return nil
	}

	var st *state.Store

	// NOTE(joel): Load state if state file is specified and enabled.
	if cfg.StateFile != "" {
		if !cfg.DisableState {
			cfg.Logger.Printf("using state file: %s", cfg.StateFile)
			st = state.New(cfg.StateFile)
			if err := st.Load(); err != nil {
				cfg.Logger.Printf("state load warning: %v", err)
			}
		} else {
			cfg.Logger.Printf("-no-state set: ignoring existing state file and forcing full emit")
		}
	}

	// NOTE(joel): Initial scan to find existing *.RDY files.
	matches, err := scanner.Scan(
		cfg.RootDir,
		scanner.Options{
			Recursive:      cfg.Recursive,
			FollowSymlinks: cfg.FollowSymlinks,
		},
	)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	// TODO: Emitted/skipped should track missing folders too.

	// NOTE(joel): Build matchedFiles output considering existing state: skip any
	// *.RDY files already recorded.
	matchedFiles := make([]scanner.Match, 0, len(matches))
	skipped := 0
	emitted := 0
	for _, m := range matches {
		// NOTE(joel): Corresponding folder is missing: skip.
		if m.MissingFolder || m.Folder == "" {
			cfg.Logger.Printf("skip (missing folder): %s", m.ReadyFile)
			skipped++
			continue
		}

		if st != nil {
			// NOTE(joel): We re-emit a *.RDY file if its modTime has changed since
			// first observation. This allows a workflow where the triggering file is
			// "touched" or rewritten to signal re-processing.
			var curMod int64 = 1
			if fi, err := os.Stat(m.ReadyFile); err == nil {
				curMod = fi.ModTime().UnixNano()
			} else {
				cfg.Logger.Printf("stat warning: %s: %v", m.ReadyFile, err)
			}

			if prev, ok := st.Get(m.ReadyFile); ok {
				if prev == curMod {
					// NOTE(joel): Unchanged since last emission: skip.
					cfg.Logger.Printf("skip (unchanged): %s", m.ReadyFile)
					skipped++
					continue
				}
				// NOTE(joel): Mod time changed: emit.
				cfg.Logger.Printf("emit (changed): %s", m.ReadyFile)
			}
		}

		// NOTE(joel): No state or not seen before: emit.
		matchedFiles = append(matchedFiles, m)
		cfg.Logger.Printf("emit (new): %s", m.ReadyFile)
		emitted++
	}

	// NOTE(joel): If configured, upload each emitted folder (only those actually
	// emitted this run) to GCS instead of emitting JSON lines to stdout.
	if cfg.GCSBucket != "" {
		u, err := uploader.NewGCS(
			context.Background(), cfg.GCSBucket, cfg.FileConcurrency,
		)
		if err != nil {
			cfg.Logger.Printf("gcs init warning: %v", err)
			return nil
		}
		defer u.Close()

		// NOTE(joel): If Firestore collection is configured, create a Firestore
		// client to record uploaded folder metadata.
		var fs *uploader.Firestore
		if cfg.FirestoreCollection != "" {
			fs, err = uploader.NewFirestore(context.Background(), cfg.FirestoreProjectId)
			if err != nil {
				cfg.Logger.Printf("firestore init warning: %v", err)
				fs = nil
			}
			defer fs.Close()
		}

		// NOTE(joel): Build folder upload tasks.
		var tasks []app.Task
		for _, m := range matchedFiles {
			tasks = append(tasks, func(ctx context.Context) error {
				filesMeta, err := u.UploadListedEntries(m.FolderEntries, "")
				if err != nil {
					cfg.Logger.Printf("gcs upload warning: folder=%s err=%v", m.Folder, err)
					return nil
				}

				// NOTE(joel): Write folder record to Firestore if configured and
				// upload was successful.
				if fs != nil {
					// NOTE(joel): Derive a relative folder path (to the configured root
					// directory) so Firestore documents don't store machine-specific
					// absolute paths.
					relFolder := m.Folder
					if rel, err := filepath.Rel(cfg.RootDir, m.Folder); err == nil && rel != "." && rel != "" {
						relFolder = rel
					}
					rec := uploader.FolderRecord{
						FolderPath: relFolder,
						UploadedAt: time.Now(),
						Files:      filesMeta,
					}
					if err := fs.WriteFolderRecord(cfg.FirestoreCollection, rec); err != nil {
						cfg.Logger.Printf("firestore write warning: folder=%s err=%v", m.Folder, err)
						return nil
					}
				}

				// NOTE(joel): Update state to mark *.RDY file as processed only after
				// successful upload (and Firestore write if configured).
				// If state is disabled, this step is skipped.
				// If the *.RDY file is missing now, we skip updating state to avoid
				// re-emission on next run (since we have already uploaded the
				// corresponding folder entries).
				if st != nil {
					if fi, err := os.Stat(m.ReadyFile); err == nil {
						st.Set(m.ReadyFile, fi.ModTime().UnixNano())
					} else {
						st.Set(m.ReadyFile, 1)
					}
				}
				return nil
			})
		}
		if len(tasks) > 0 {
			if err := app.RunParallel(
				context.Background(), cfg.FolderConcurrency, tasks,
			); err != nil {
				cfg.Logger.Printf("gcs folder upload warning: %v", err)
			}
		}
	} else {
		// NOTE(joel): Emit initial set of matches as JSON lines to stdout.
		enc := json.NewEncoder(cfg.Stdout)
		if len(matchedFiles) > 0 {
			if err := enc.Encode(matchedFiles); err != nil {
				return fmt.Errorf("encode initial: %w", err)
			}
		}
	}

	// NOTE(joel): Update last run timestamp after initial emit (if any).
	// This ensures that even if no new files were emitted, the state file's
	// timestamp reflects the last time local-file-sync was run.
	// If state is disabled, this step is skipped.
	if st != nil {
		st.SetLastRun(time.Now())
		if err := st.Save(); err != nil {
			cfg.Logger.Printf("state save warning: %v", err)
		}
	}

	cfg.Logger.Printf(
		"summary: scanned=%d emitted=%d skipped=%d",
		len(matches), emitted, skipped,
	)

	return nil
}
