# AI Assistant Project Instructions

Concise, project-specific guidance for AI coding agents working on `local-file-sync` (RDY Scanner + optional GCS / Firestore uploader). Focus on THESE patterns—avoid generic Go boilerplate.

## 1. Purpose & Data Flow
Scan a root directory for case-insensitive `*.RDY` files. For each `NAME.RDY`:
1. Determine sibling directory `NAME/` (same folder, same basename).
2. Capture non-recursive listing of immediate child entries (sorted by name).
3. Either emit all matches as a single JSON array to stdout OR (if `-gcs-bucket` set) upload each newly emitted folder's immediate files to GCS (suppress JSON). Optional Firestore doc per uploaded folder.
4. Persistent state suppresses re-emitting unchanged RDY triggers (mod-time based). Updating the RDY file's mtime retriggers processing.

## 2. Key Packages / Responsibilities
- `cmd/local-file-sync/main.go`: Flag parsing via `app.ParseFlags()`, lock acquisition, orchestrates scan -> state-based filtering -> emit OR upload -> state save.
- `internal/app/config.go`: Flag definitions, derived defaults (state file path & lock hash). Preserve backward compatibility; new flags default to neutral behavior.
- `internal/app/lock.go`: File lock (stale after 30m) to prevent overlapping runs on same root; reclaim if stale, silent skip if active.
- `internal/app/workerpool.go`: `RunParallel` (auto concurrency clamp 2..8). First error cancels remaining tasks.
- `internal/scanner/scanner.go`: Finds case-insensitive `.RDY` files; optional recursion & symlink following; deterministic ordering of matches and folder entries.
- `internal/state/state.go`: Atomic JSON (`version`, `last_run`, `files[path]=modTimeNS`). `LastRun` always updated even if no new matches. Skip logic uses strict equality on stored modTime.
- `internal/uploader/gcs.go`: Non-recursive upload of provided `FolderEntries` (ignores dirs, symlinks, `.RDY`). Builds object name `<basename(folder)>/<filename>` (allowing a future prefix). Per-file SHA256 via `getChecksum`; MIME via `detectContentType`; concurrency using worker pool.
- `internal/uploader/firestore.go`: When `-firestore PROJECT:COLLECTION` + `-gcs-bucket` set, writes one document per successfully uploaded folder. Document schema: `{ folderPath, uploadedAt, files[] }` where `files[]` mirrors `UploadedFile` (`name,size,checksum,path`). Document ID is a deterministic 20-char base64url string from first 15 bytes of SHA256(folderPath) (`hashPath`)—avoid collisions & keeps stable IDs for idempotent re-uploads. Write occurs only after successful GCS upload; failure logs warning but does not abort other folders.

## 3. Conventions & Invariants
- Sorting: RDY file list (`sort.Strings`) and folder entries (`sort.Slice` by name) must remain deterministic for stable JSON diffs & reproducible uploads.
- State skip rule: Only skip if stored modTime equals current modTime. If modTime differs, treat as new (re-emit or re-upload) and overwrite stored value.
- When uploading: State for a RDY file is updated only after successful folder upload (and Firestore write if enabled). JSON emission path updates state before encoding.
- Missing folder: Represented as `"missingFolder": true`; do NOT error the whole run.
- Lock semantics: If lock not acquired (held & not stale) exit 0 after logging; produce no output and perform no uploads.
- All emitted JSON: Single line array (no pretty print) only if at least one match.
- Case insensitivity: Always compare `strings.ToUpper(name)` for `.RDY` suffix.

## 4. Adding Features Safely
When adding features ensure:
- Maintain backward-compatible flags; new flags must default to no behavior change.
- Preserve deterministic ordering (add sorting if new collections introduced).
- Do not make network calls unless `-gcs-bucket` (and maybe `-firestore`) are set.
- Keep uploads non-recursive unless a new explicit flag enables recursion (then document clearly and guard default behavior).
- Extend state file schema via version bump ONLY if necessary; maintain read of old schema.

## 5. Testing Focus (see existing *_test.go files)
- Lock tests rely on injectable clock via `acquireLockWith`.
- Worker pool tests expect bounded concurrency & early cancel on first error.
- Scanner tests validate case-insensitive detection & symlink handling toggled by flags.
- State tests assert atomic save, `LastRun` updates even with no new files.
- Uploader tests may use `fileUploadHook` for deterministic behavior (avoid real GCS).

## 6. Common Tasks (Taskfile.sh)
Use `./Taskfile.sh`:
- `format`: go fmt ./...
- `lint`: golangci-lint run ./...
- `test`: go test ./internal/... -cover
- `build`: cross-compiles w/ `-ldflags "-X main.version=$VERSION"` into `./bin/`
- `validate`: lint + test
Install linter first if missing: `./Taskfile.sh install_dependencies`.

## 7. External Dependencies
- GCS: Requires Application Default Credentials (`GOOGLE_APPLICATION_CREDENTIALS` service account JSON or `gcloud auth application-default login`). Failure to init logs warning & skips uploads (run otherwise ends successfully).
- Firestore: Only initialized if both `-gcs-bucket` and `-firestore PROJECT:COLLECTION` provided. Project & collection parsed via `PROJECT:COLLECTION`. Failed init logs warning; run proceeds without Firestore. Each folder record written with hashed ID (stable per folder path) to simplify upserts; no partial retries implemented—failure leaves state un-updated only if upload also failed.
- Go version: `go 1.25.0` (avoid newer language features unless bumping module).

## 8. Patterns to Reuse
- Concurrency: Use `app.RunParallel(ctx, desiredConcurrency, []app.Task{...})`; keep tasks side-effect isolated & idempotent where possible.
- Checksums: `getChecksum` (SHA256) already used for GCS uploads & Firestore metadata—reuse for any integrity features.
- Content type: Extend `detectContentType` (lowercase ext switch) rather than ad-hoc MIME guesses.
- Prefix computation: Extend `makePrefixGetter` for any future hierarchical or user-specified object prefix logic (memoization ensures O(1) reuse per dir).
- Firestore IDs: Derive stable IDs with `hashPath` if new collections added—keeps document naming uniform.

## 9. Pitfalls / Edge Cases
- Symlink loops only possible if `-follow-symlinks` with recursive; current code skips symlink dirs unless flag set. Maintain this safeguard.
- Race: Files disappearing between scan and stat/upload should be silently skipped (current logic tolerates missing / changed files without fatal).
- Empty matches: Emit nothing (no `[]`) when zero matches. Preserve this contract.
- State disabled (`-no-state`): Always emit (or upload) every RDY file each run; never write state.

## 10. Extension Ideas (Gate Behind Flags)
- Recursive folder uploads: new flag (e.g. `-gcs-recursive`).
- Checksums caching in state (would require schema version bump).
- Firestore indexing / query enhancements.

Keep these instructions updated when modifying data flow, flag semantics, state schema, or upload logic.
