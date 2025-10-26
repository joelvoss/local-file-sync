# Changelog

All notable changes to this project will be documented in this file.

## v0.0.1 - 2025-10-26
- Initial release of `local-file-sync`.
- Scans directories for `.RDY` trigger files and lists sibling folder contents.
- Emits deterministic JSON summaries or uploads folder files to GCS when configured.
- Maintains state to suppress unchanged reruns and supports Firestore metadata records.
