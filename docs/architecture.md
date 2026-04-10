# Architecture

## Goal

GoogleTakeoutFixer is designed as a deterministic archive-repair tool for Google Photos Takeout exports. The core idea is that a run should be explainable, resumable, and safe to re-run.

## Pipeline

1. Validate input/output paths and runtime dependencies.
2. Discover media and JSON sidecars into a `MediaPlan`.
3. Resolve matches using deterministic filename and metadata heuristics.
4. Restore metadata, deduplicate exact copies, and write audit state.
5. Verify written metadata when enabled.
6. Emit machine-readable and human-readable reports under `OUTPUT/.gtf/`.

## Matching Algorithm

The matcher lives in `internal/fixer/matcher.go`.

It intentionally avoids “first JSON with same prefix wins” behavior. Instead it ranks candidates in this order:

1. Exact `<media>.json` name match.
2. JSON `title` that exactly matches the media filename.
3. Normalized name matches that account for:
   - duplicate suffixes like `name(1).jpg` and `name.jpg(1).json`
   - edited variants like `-edited`
   - long-name truncation in Takeout JSON exports
4. Live-photo / partner-file fallback for videos that share metadata with a sibling image.

If multiple candidates tie at the best score, the file is marked `ambiguous` and reported instead of silently picking one.

## State Model

`internal/fixer/state.go` stores append-only JSONL records in `OUTPUT/.gtf/state.jsonl`.

This state enables:

- resumable runs
- idempotent reruns
- exact-content deduplication across year and album folders
- auditability after partial failures

Corrupt lines are ignored with warnings instead of crashing the run.

## Metadata Strategy

Metadata handling is in `internal/fixer/metadata_handler.go`.

Important rules:

- JSON is treated as the authoritative migration source unless the user picks another conflict policy.
- GPS coordinates and timezone offsets are restored together when possible.
- Video writes target QuickTime/Keys/XMP tags instead of relying on photo-only EXIF tags.
- Verification reads metadata back with ExifTool after write when `--verify` is enabled.

## Runtime Paths

Runtime artifacts are split by purpose:

- User preferences: user config directory, `GoogleTakeoutFixer/config.json`
- Per-run state, logs, reports: `OUTPUT/.gtf/`

This keeps logs and reports attached to the repaired library rather than the current working directory.
