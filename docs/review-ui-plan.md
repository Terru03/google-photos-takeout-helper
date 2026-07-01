# Review UI Plan

This pass makes review data easier to reach from the GUI by linking `reports/review.csv`, `reports/suspicious_dates.csv`, `reports/latest.txt`, and `reports/latest.json`.

The next larger GUI pass should add a dedicated review table with these filters:

- unmatched media
- ambiguous media
- suspicious dates
- verification failures
- metadata conflicts

Useful row actions:

- open media file
- open JSON sidecar
- open containing folder
- copy source/output path
- export current filter to CSV
- choose a manual JSON match and re-run only that file

Manual matching should stay non-destructive. It should write a small override file under `OUTPUT/.gtf/` and require a re-run to apply the match, instead of editing source Takeout folders.

## XMP follow-up

Current XMP mode writes a compact `media.ext.xmp` sidecar with capture date, title, description, and GPS. A deeper pass can add richer namespaces, album/person fields if available, and an optional ExifTool-based XMP writer for users who want stricter tool-generated sidecars.
