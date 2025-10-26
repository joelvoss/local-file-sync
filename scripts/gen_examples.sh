#!/usr/bin/env bash
set -euo pipefail
# Regenerate example dataset
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXAMPLE="$ROOT/example"
rm -rf "$EXAMPLE"
mkdir -p "$EXAMPLE"

# Basic case with folder
echo "" > "$EXAMPLE/ORDER100.RDY"
mkdir -p "$EXAMPLE/ORDER100"
echo "payload for order 100" > "$EXAMPLE/ORDER100/data.txt"
echo "additional notes" > "$EXAMPLE/ORDER100/notes.md"
echo "log line 1" > "$EXAMPLE/ORDER100/0001.log"
echo "temp work" > "$EXAMPLE/ORDER100/work.bin"

# RDY without corresponding folder (missing folder scenario)
echo "" > "$EXAMPLE/ORDER200.RDY"

# Folder without RDY (should be ignored)
mkdir -p "$EXAMPLE/ORDERNOTREADY200"
echo "payload for order 200" > "$EXAMPLE/ORDERNOTREADY200/data.txt"
echo "additional notes" > "$EXAMPLE/ORDERNOTREADY200/notes.md"

# Nested recursive case
mkdir -p "$EXAMPLE/nested/INNER300"
echo "" > "$EXAMPLE/nested.RDY"
echo "" > "$EXAMPLE/nested/INNER300.RDY"
echo "outer nested data" > "$EXAMPLE/nested/data.txt"
echo "nested work payload" > "$EXAMPLE/nested/work.bin"
echo "inner 300 file" > "$EXAMPLE/nested/INNER300/info.txt"
echo "v1" > "$EXAMPLE/nested/INNER300/version.txt"
echo "binarydata" > "$EXAMPLE/nested/INNER300/blob.dat"
echo "fake image" > "$EXAMPLE/nested/INNER300/pic.png"

# Symlink test (only used when -follow-symlinks)
ln -s "nested" "$EXAMPLE/link-to-nested" || true

# Mixed case suffix test
echo "" > "$EXAMPLE/MixedCase.rDy"
mkdir -p "$EXAMPLE/MixedCase"
echo "mixed case file" > "$EXAMPLE/MixedCase/file.txt"
echo "another" > "$EXAMPLE/MixedCase/another.txt"
echo "deep" > "$EXAMPLE/MixedCase/deep.txt"

printf "Example dataset regenerated at %s\n" "$EXAMPLE"
