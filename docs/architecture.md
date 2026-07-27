# Architecture

## Goal

Google Photos Takeout Helper is designed as a deterministic archive-repair tool for Google Photos Takeout exports. The core idea is that a run should be explainable, resumable, and safe to re-run.

## Pipeline

1. Validate input/output paths and runtime dependencies.
2. Index JSON sidecars across every selected split ZIP, then discover media into a `MediaPlan`.
3. Resolve matches using deterministic filename and metadata heuristics.
4. Restore metadata to output files and/or XMP sidecars, deduplicate exact copies, and write audit state.
5. Verify written metadata when enabled.
6. Commit the completed per-ZIP SSD staging tree to final output.
7. After all ZIPs, optionally run the resumable MotionPhoto2 merge pass with one timeout per pair.
8. Emit machine-readable, human-readable, and CSV review reports under `OUTPUT/.gtf/`.

## Matching Algorithm

The matcher lives in `internal/fixer/matcher.go`.

It intentionally avoids “first JSON with same prefix wins” behavior. Instead it ranks candidates in this order:

1. Exact supplemental-metadata duplicate ordinal match, such as media `(1)` to sidecar `(1)`.
2. Exact `<media>.json` name match.
3. JSON `title` that exactly matches the media filename.
4. Normalized name matches that account for:
   - duplicate suffixes like `name(1).jpg` and `name.jpg(1).json`
   - edited variants like `-edited`
   - long-name truncation in Takeout JSON exports
5. Live-photo / partner-file fallback for sibling photo/video pairs, with sidecar inheritance in either direction when only one side matches directly.

If multiple candidates tie at the best score, the file is marked `ambiguous` and reported instead of silently picking one.

The batch-sidecar index reads only JSON entries from all selected ZIPs. This lets media in one split ZIP use Google metadata from another split ZIP without keeping every large ZIP extracted at once.

Partner relationships are also recorded in the audit output even when neither side has a JSON sidecar, so live/motion-photo exports remain explainable instead of looking like unrelated misses.

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
- XMP sidecar mode writes `media.ext.xmp` beside output media instead of modifying media bytes when the user selects sidecar-only output.
- Verification reads metadata back with ExifTool after write when `--verify` is enabled.

## Motion Photo Strategy

Phase 1 never calls MotionPhoto2. It keeps still and motion-video companions as normal output files.

`--merge-motion-pass` scans the finished library, merges one pair per process, applies a timeout, continues after failure, and stores resume state under `.gtf`.

## Performance Strategy

- One persistent ExifTool process serves a whole ZIP instead of starting Perl for every metadata call.
- State records buffer and sync in batches, then sync on clean close.
- GUI and file progress logs refresh at bounded intervals.
- SSD staging keeps metadata rewrites off the final HDD and commits only a completed ZIP.

## Runtime Paths

Runtime artifacts are split by purpose:

- User preferences: user config directory, `GoogleTakeoutFixer/config.json` for compatibility with older installs
- Per-run state, logs, reports: `OUTPUT/.gtf/`

This keeps logs and reports attached to the repaired library rather than the current working directory.

## Proof Report

Each run writes `reports/latest.txt`, `reports/latest.json`, `reports/review.csv`, and `reports/suspicious_dates.csv`. The summary counts input media, JSON sidecars, clean matches, fallback matches, ambiguous and unmatched files, duplicate reuse, motion-photo work, metadata writes, verification failures, output media, and artifact paths.
