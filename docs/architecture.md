# Architecture

## Goal

GoogleTakeoutFixer is designed as a deterministic archive-repair tool for Google Photos Takeout exports. The core idea is that a run should be explainable, resumable, and safe to re-run.

## Pipeline

1. Validate input/output paths and runtime dependencies.
2. Discover media and JSON sidecars into a `MediaPlan`.
3. Resolve matches using deterministic filename and metadata heuristics.
4. Restore metadata, deduplicate exact copies, and write audit state.
5. Verify written metadata when enabled.
6. Optionally run a post-processing MotionPhoto2 sweep to rebuild Windows-viewable Samsung/Google Motion Photos in-place.
7. Emit machine-readable and human-readable reports under `OUTPUT/.gtf/`.

## Matching Algorithm

The matcher lives in `internal/fixer/matcher.go`.

It intentionally avoids “first JSON with same prefix wins” behavior. Instead it ranks candidates in this order:

1. Exact `<media>.json` name match.
2. JSON `title` that exactly matches the media filename.
3. Normalized name matches that account for:
   - duplicate suffixes like `name(1).jpg` and `name.jpg(1).json`
   - edited variants like `-edited`
   - long-name truncation in Takeout JSON exports
4. Live-photo / partner-file fallback for sibling photo/video pairs, with sidecar inheritance in either direction when only one side matches directly.

If multiple candidates tie at the best score, the file is marked `ambiguous` and reported instead of silently picking one.

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
- Verification reads metadata back with ExifTool after write when `--verify` is enabled.

## Motion Photo Strategy

When `CreateMotionPhotos` / `--motion-photos` is enabled, GoogleTakeoutFixer runs [MotionPhoto2](https://github.com/PetrVys/MotionPhoto2) against the repaired output tree using recursive overwrite, incremental mode, and EXIF-based matching.

This design choice keeps the main Takeout repair pipeline deterministic while delegating platform-specific live-photo remuxing to a tool that already targets Samsung/Google Motion Photo compatibility for Windows Photos.

## Runtime Paths

Runtime artifacts are split by purpose:

- User preferences: user config directory, `GoogleTakeoutFixer/config.json`
- Per-run state, logs, reports: `OUTPUT/.gtf/`

This keeps logs and reports attached to the repaired library rather than the current working directory.
