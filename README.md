# local-file-sync

Utility to scan a directory tree for files ending (case‑insensitive) with
`*.RDY` and list the contents of any sibling directory that shares the same base
name. Optionally, newly detected matched folders can have their immediate
top‑level files uploaded to Google Cloud Storage (and, if configured, a
Firestore document recorded) instead of emitting JSON.

## Features

- Case‑insensitive scan for `.RDY` files (`MixedCase.rDy` works) under a root
  directory (optionally recursive; optional symlink following when recursive).
- For each `NAME.RDY`, identify a sibling directory `NAME/` (non-recursive
  listing only) and capture its immediate entries.
- Deterministic, single‑line JSON array output describing all emitted matches
  (suppressed when `-gcs-bucket` is set).
- Optional direct upload of each newly emitted folder's immediate regular files
  to Google Cloud Storage (non-recursive) with stable object naming:
  `<basename(folder)>/<filename>`.
- Optional Firestore document per successfully uploaded folder
  (`-firestore PROJECT:COLLECTION`) containing folder path (relative to scan
  root), timestamp, and per-file metadata (name, size, checksum, object path).
- Per‑file SHA256 checksum recorded when uploading; checksum reused in Firestore
  docs.
- Persistent state suppresses unchanged `.RDY` triggers (modTime based).
  Touching / rewriting the `.RDY` file retriggers emission or upload.
- Missing / unreadable sibling folder represented with `"missingFolder": true`
  (run continues).
- Stable ordering: list of `.RDY` files (`sort.Strings`) and folder entries
  (sorted by name) for reproducible output and uploads.
- Process lock prevents concurrent overlapping runs for same root; stale (>30m)
  lock reclaimed; active lock => clean no‑op exit.
- Graceful skipping of disappearing files during upload (individual file issues
  don't abort other folders).
- Explicit concurrency controls: folder task concurrency (`-folder-concurrency`)
  and per‑file upload concurrency (`-file-concurrency`) with auto clamping
  when 0.

### What This Tool Does NOT (Yet) Do

- No recursive folder uploads (only top-level files).
- No deletion / sync pruning in GCS; uploads are additive.
- No checksum diffing against existing bucket objects.
- No partial retry for failed Firestore writes (failure is logged, run
  continues).

## Development / Task Runner

This repository includes a lightweight task runner script `Taskfile.sh` that
wraps common developer actions (formatting, linting, testing, building
multi‑platform artifacts, etc.). You can invoke any task by passing its name:

```bash
./Taskfile.sh help
```

Key tasks:

| Task                   | Description                                                    |
| ---------------------- | -------------------------------------------------------------- |
| `format`               | Run `go fmt ./...`                                             |
| `lint`                 | Run `golangci-lint run ./...` (installs separately)            |
| `test`                 | Run unit tests with coverage for internal pkgs                 |
| `build`                | Produce binaries for several OS/ARCH combinations into `./bin` |
| `validate`             | Runs `lint` then `test`                                        |
| `install_dependencies` | Installs helper tools (`golangci-lint`)                        |

Examples:

```bash
# Run lint & tests
./Taskfile.sh validate

# Just run lint
./Taskfile.sh lint

# Build release binaries
./Taskfile.sh build
```

If `golangci-lint` is not installed, run the dependency installer first:

```bash
./Taskfile.sh install_dependencies
```

> Note: The build task sets a `-ldflags "-X main.version=$VERSION"`.

## Usage

```bash
local-file-sync -dir /path/to/scan                # write JSON array to stdout
local-file-sync -dir /path/to/scan > output.json  # redirect output to a file
local-file-sync -dir /path/to/scan -recursive     # include subdirectories
local-file-sync -dir /path/to/scan -recursive -follow-symlinks  # follow symlinked directories
local-file-sync -dir /path/to/scan -gcs-bucket my-bucket        # upload matched folders' top-level files to GCS (suppresses JSON)
local-file-sync -dir /path/to/scan -gcs-bucket my-bucket -firestore myproj:uploads  # also write Firestore docs
```

Key flags:

```
-dir string              Directory to scan (default ".")
-recursive               Recursively scan for *.RDY files (case-insensitive match)
-follow-symlinks         Follow directory symlinks (only meaningful with -recursive)
-state-file string       Path to persistent state file (default: <dir>/.local-file-sync_state.json)
-no-state                Disable state entirely (ignore any existing state; emit all RDY files every run; no writes)
-lock-file string        Path to lock file (default: /tmp/local-file-sync-<hash>.lock derived from -dir)
-gcs-bucket string       If set, upload each newly emitted matched folder's immediate (non-recursive) files to the given GCS bucket (suppresses JSON output)
-firestore string        PROJECT:COLLECTION to record one document per successfully uploaded folder (requires -gcs-bucket)
-folder-concurrency int  Max concurrent folder upload tasks (0=auto; applies only when -gcs-bucket)
-file-concurrency int    Max concurrent file uploads per folder (0=auto; applies only when -gcs-bucket)
```

Exit behavior:

- Non-zero exit code on fatal errors (e.g. unreadable root).
- Zero exit code if another process already holds the lock (a notice is logged
  and no JSON is emitted or files uploaded).

## Tests

```bash
./Taskfile.sh test
```

## JSON Output Schema

Each run emits exactly one JSON array (pretty printing is not used). Elements
have the shape (one object per detected `*.RDY` file):

```jsonc
{
  "readyFile": "/abs/path/ORDER123.RDY", // absolute path to the .RDY file
  "folder": "/abs/path/ORDER123", // omitted if folder missing
  "missingFolder": false, // true if folder absent or unreadable
  "folderEntries": [ // omitted if missingFolder true
    {
      "name": "file.txt",
      "size": 1234,
      "modTime": "2025-09-09T12:34:56.789012Z",
      "path": "/abs/path/ORDER123/file.txt"
    }
  ]
}
```

## Repeated Runs

Invoke `local-file-sync` periodically. With state enabled (default) a `.RDY`
file is skipped if its last observed modTime matches the stored modTime.
Changing the timestamp (e.g. `touch file.RDY`) forces re‑emission (JSON mode) or
re‑upload logic to run again. Use `-no-state` to force emission / upload every
run.

## State File Format

By default a `.local-file-sync_state.json` file is stored in the scanned
directory unless you specify `-state-file` or disable state with `-no-state`.

Schema:

```jsonc
{
  "version": 1,
  "last_run": "2025-09-10T12:34:56.789012345Z",
  "files": {
    "/abs/path/ORDER100.RDY": 1694958896789012345,
    "/abs/path/ORDER200.RDY": 1694958897790123456
  }
}
```

`files` maps each absolute RDY file path to the most recently observed
modification time of the trigger (Unix nanoseconds). If a `.RDY` file's
modification time changes between runs (e.g. the file is re-written or
`touch`-ed) it is treated as a new signal and will be re‑emitted. This allows a
producer to re-trigger downstream processing by updating the timestamp of the
ready file. Unchanged `.RDY` files are skipped to avoid duplicate work.

Note: The state file is rewritten on every run. This guarantees the on-disk
`last_run` always reflects the most recent invocation, even if no new `.RDY`
files were discovered. Only new triggers cause additions to `files`; existing
entries are unchanged.

## Example Dataset

The `example/` folder includes sample cases:

- `ORDER100.RDY` with matching directory `ORDER100/` (contains files like
  `data.txt`).
- `ORDER200.RDY` without a matching directory (demonstrates
  `missingFolder: true`).
- `MixedCase.rDy` showing case-insensitive extension handling with folder
  `MixedCase/`.
- Nested example (`nested/INNER300.RDY` + directory) to illustrate recursive
  scanning.

## Google Cloud Storage Uploads

If you provide `-gcs-bucket <bucket>`, every folder associated with an emitted
`.RDY` trigger has only its immediate (top‑level) regular files uploaded
(non‑recursive). JSON output is suppressed in this mode. Object naming:

```
<bucket>/<basename(folder)>/<filename>
```

Notes:

- Credentials: Requires Application Default Credentials (ADC). Set
  `GOOGLE_APPLICATION_CREDENTIALS` to a service account JSON key file OR run
  `gcloud auth application-default login`.
- Scope: Only immediate regular files are uploaded; directories, symlinks, and
  the `.RDY` file itself are ignored.
- Failures: Per-file failures inside a folder abort that folder's upload task;
  other folders proceed. Individual missing files encountered mid-upload are
  skipped.
- State timing: In upload mode, the state is updated for a `.RDY` file only
  after a successful folder upload (and Firestore write if enabled). This
  prevents marking a trigger complete if its upload failed.
- Concurrency: Folder uploads run concurrently (bounded by
  `-folder-concurrency`); inside each folder, file uploads are concurrent
  (bounded by `-file-concurrency`).

### Firestore Integration

Add `-firestore PROJECT:COLLECTION` (must accompany `-gcs-bucket`) to persist a
document per successfully uploaded folder. Schema:

```jsonc
{
  "folderPath": "relative/path/from/root",
  "uploadedAt": "2025-09-30T12:34:56Z",
  "files": [
    {
      "name": "file.txt",
      "size": 1234,
      "checksum": "<sha256>",
      "path": "FOLDER/file.txt"
    }
  ]
}
```

Document IDs are deterministic: first 15 bytes of SHA‑256 of `folderPath`,
base64url encoded (20 chars). This allows idempotent re-uploads (same folder
path overwrites the same doc). A Firestore write failure logs a warning but does
not invalidate the overall run; the state will not be marked updated if upload
itself failed.

### Content Types & Checksums

Uploads assign a simple MIME type based on file extension (text, images,
documents, archives, etc.). Unknown types default to `application/octet-stream`.
Each uploaded file's SHA256 checksum is computed and stored in Firestore
metadata (when enabled).

## Summary Logging

At the end of each run a log line summarizes counts: scanned (total `.RDY`
triggers located), emitted (those processed this run), and skipped (those
suppressed by state).
