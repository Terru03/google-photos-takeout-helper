# Google Photos Takeout Helper

<p align="center">
  <img src="images/GoogleTakeoutFixer.png" alt="GoogleTakeoutFixer logo" width="200"/>
</p>

**Google Photos Takeout Helper** is a local Google Photos Takeout repair and migration tool for Windows. The maintained executable is **`GoogleTakeoutFixer.exe`**.

It is designed for messy and very large Google Photos exports, including split Takeout ZIP archives where a media file and its Google JSON sidecar may be stored in different ZIPs.

The app can restore capture metadata, organise a clean timeline library, deduplicate exact copies, repair misleading file extensions, preserve Motion Photo pairs, verify metadata writes, resume interrupted ZIP batches, and produce detailed audit reports without modifying the original Takeout archives.

## What it does

### Google metadata matching

- Indexes Google Photos JSON sidecars across **all selected split Takeout ZIPs** before processing.
- Lets media in one ZIP use its matching Google metadata from another ZIP.
- Matches duplicate ordinals such as `photo(1).jpg` with the corresponding `(1)` supplemental metadata.
- Handles exact filename/title matches, Takeout duplicate naming, edited variants and long-name truncation.
- Uses photo/video partner fallback for Live Photo and Motion Photo pairs.
- Reports ambiguous matches instead of silently choosing one.
- Ignores non-media Google JSON such as `user-generated-memory-titles.json` when building the media sidecar index.

### Metadata repair

- Writes Google capture metadata into supported photos and videos using **ExifTool**.
- Can verify written metadata by reading it back with `--verify`.
- Supports metadata conflict handling with `prefer-json`, `prefer-embedded`, or `merge`.
- Can write metadata to the media file, XMP sidecars, both, or neither.
- If a direct metadata write fails, the tool restores the original media bytes and attempts an **XMP sidecar fallback** instead of leaving a partially modified file.
- Detects common files whose extension does not match their real content and corrects JPEG, PNG and HEIC names from the file signature.
- Can restore `.MOV` when embedded metadata identifies Apple QuickTime content.

### Library organisation

Batch mode defaults to a date-based timeline layout with month subfolders.

Date selection is intentionally conservative and uses this order:

1. Google `photoTakenTime.timestamp`, including metadata indexed from another selected Takeout ZIP.
2. Embedded EXIF/XMP/media creation time.
3. A valid date encoded in the media filename.
4. The Google timeline folder year, with `Unknown month` when the year is known but the month is not safe to infer.
5. File modified time only when no Google timeline year is available.

Albums do not replace the primary timeline date. Album output can be kept separately when wanted.

### Large split-ZIP processing

- Processes Takeout ZIPs sequentially, one ZIP at a time, instead of extracting the entire archive set at once.
- Supports multiple temporary work folders so the app can choose a suitable location with enough free space.
- Supports optional **SSD staging output** so metadata work happens on fast storage before a completed ZIP is committed to the final library.
- Keeps a resumable manifest at `OUTPUT\.gtf\batch\manifest.jsonl`.
- Automatically skips ZIPs already completed successfully unless `--reprocess` is used.
- Marks previously interrupted runs and retries them safely.
- Can deliberately skip extracting a problematic ZIP with `--skip-zip` while still indexing usable JSON sidecars from it.
- Keeps review items visible rather than treating every imperfect match as a fatal batch failure.

### Motion Photos

Phase 1 preserves the still image and companion `.MOV`/`.MP4` files. It does not invoke MotionPhoto2 while individual ZIPs are being repaired.

After the library is complete, the optional Motion Photo pass can:

- rebuild compatible Samsung/Google Motion Photos using **MotionPhoto2**;
- process one pair at a time with a configurable timeout;
- continue after a failed pair;
- skip already successful pairs on rerun;
- retry only failed or timed-out pairs;
- write a separate resume/report file under `.gtf`.

## Safety model

The tool is deliberately conservative.

- Original Takeout ZIP files are never moved, modified, or deleted by batch mode.
- Output is written only to the selected output or staging folders.
- Temporary extraction stays inside the selected work folder or work pool.
- ZIP paths are sanitised to prevent traversal and unsafe Windows names.
- Exact duplicates are handled deterministically.
- Ambiguous matches, suspicious dates, conflicts and metadata failures are reported for review.
- A failed metadata write does not intentionally leave modified media bytes behind.
- `--dry-run` can audit a library without writing repaired media.

Always keep the original Google Takeout archives until the repaired library and reports have been checked.

## Desktop app

Run `GoogleTakeoutFixer.exe` with no arguments to open the Windows desktop interface.

For a large export, the usual workflow is:

1. Add the folders containing the Google Takeout ZIPs.
2. Select the final output folder.
3. Select one or more temporary work folders.
4. Optionally select a fast SSD staging folder.
5. Run preflight.
6. Start the batch.
7. Review the manifest and reports after processing.

Batch mode keeps the primary output timeline-based by default and preserves a saved Motion Photo setting instead of silently disabling it.

## Install

If a Windows build is available under **Releases**, download the archive, extract it and run:

```text
GoogleTakeoutFixer.exe
```

Windows SmartScreen may warn about unsigned community builds. Verify the repository and release checksums where provided rather than disabling security protections.

### External tools

**ExifTool** is required when writing or verifying embedded metadata.

**MotionPhoto2** is optional and only required when rebuilding Motion Photos. It can be installed separately, placed beside the app, supplied with `--motionphoto2`, or made available on `PATH`.

## Recommended CLI examples

### Extracted Google Photos folder

```powershell
.\GoogleTakeoutFixer.exe `
  --input "D:\Takeout\Google Photos" `
  --output "B:\Google_Photos_Final" `
  --profile recommended-safe
```

### Audit only

```powershell
.\GoogleTakeoutFixer.exe `
  --profile audit-only `
  --input "D:\Takeout\Google Photos" `
  --output "B:\Google_Photos_Final"
```

### Large split-ZIP export

```powershell
.\GoogleTakeoutFixer.exe `
  --batch-zips `
  --zip-root "D:\Takeout Zips" `
  --work "C:\Takeout_Work" `
  --staging-output "C:\Takeout_Staging" `
  --output "B:\Google_Photos_Final" `
  --profile recommended-safe
```

### Multiple work folders

```powershell
.\GoogleTakeoutFixer.exe `
  --batch-zips `
  --zip-root "D:\Takeout Zips" `
  --work "C:\Takeout_Work" `
  --work "E:\Takeout_Work" `
  --output "B:\Google_Photos_Final" `
  --verify
```

You can also use a semicolon-separated pool:

```powershell
--work-pool "C:\Takeout_Work;E:\Takeout_Work"
```

### Preflight only

```powershell
.\GoogleTakeoutFixer.exe `
  --batch-zips `
  --preflight-only `
  --zip-root "D:\Takeout Zips" `
  --work "C:\Takeout_Work" `
  --output "B:\Google_Photos_Final"
```

### Process one ZIP for debugging

```powershell
.\GoogleTakeoutFixer.exe `
  --one-zip "D:\Takeout Zips\takeout-001.zip" `
  --work "C:\Takeout_Work" `
  --staging-output "C:\Takeout_Staging" `
  --output "B:\Google_Photos_Final" `
  --verify
```

### Skip extraction of one problematic ZIP

The skipped ZIP is still inspected during the global JSON sidecar indexing phase.

```powershell
.\GoogleTakeoutFixer.exe `
  --batch-zips `
  --zip-root "D:\Takeout Zips" `
  --skip-zip "takeout-005.zip" `
  --work "C:\Takeout_Work" `
  --output "B:\Google_Photos_Final"
```

`--skip-zip` may be repeated and accepts either a ZIP filename or full path.

## Output profiles

### `recommended-safe`

Designed for normal migration work:

- writes embedded metadata;
- verifies writes;
- restores MOV extensions where appropriate;
- deduplicates exact copies;
- merges compatible embedded and Google metadata;
- keeps only useful album copies instead of blindly duplicating the full timeline;
- never deletes the source automatically.

### `audit-only`

For inspection before committing to a migration:

- does not write metadata;
- does not modify output media;
- produces an audit plan/report;
- still performs duplicate and matching analysis.

### `immich`

A migration-oriented profile that:

- writes embedded metadata;
- also writes XMP sidecars;
- verifies writes;
- keeps Live/Motion Photo companion video files;
- deduplicates exact copies;
- uses merge conflict handling.

## Metadata modes

Use `--metadata-mode` when you want explicit control:

```text
file   write embedded metadata
xmp    write XMP sidecars
both   write embedded metadata and XMP sidecars
none   do not write metadata
```

## Album and layout options

Useful choices include:

```text
--timeline-only
--albums-separate
--album-mode unique-only
--album-mode timeline-only
--album-mode all
--month-subfolders
--flatten
--symlink
--ignore-albums
```

Some combinations are intentionally rejected when they would produce an unsafe or contradictory layout.

## Motion Photo merge pass

After ZIP processing has completed:

```powershell
.\GoogleTakeoutFixer.exe `
  --merge-motion-pass `
  --library "B:\Google_Photos_Final" `
  --motionphoto2 "C:\Tools\motionphoto2.exe"
```

The default timeout is two minutes per pair.

Retry only failed or timed-out pairs:

```powershell
.\GoogleTakeoutFixer.exe `
  --merge-motion-pass `
  --library "B:\Google_Photos_Final" `
  --motionphoto2 "C:\Tools\motionphoto2.exe" `
  --retry-failed-motion
```

The report is written to:

```text
OUTPUT\.gtf\reports\motion-merge-report.json
```

## Batch status and resume

Batch state is stored at:

```text
OUTPUT\.gtf\batch\manifest.jsonl
```

Typical states are:

- `completed` - ZIP processed cleanly;
- `completed_with_review` - processing finished but review items were recorded;
- `failed` - a fatal extraction, processing, disk or write failure occurred;
- `interrupted` - a previous run stopped before the ZIP completed.

Completed ZIPs are skipped on rerun. Failed or interrupted work can be retried without starting the whole archive again.

## Reports

Runtime state and reports live under:

```text
OUTPUT\.gtf\
```

Important files include:

```text
state.jsonl
batch\manifest.jsonl
batch\preflight_latest.txt
reports\latest.txt
reports\latest.json
reports\suspicious_dates.csv
reports\motion-merge-report.json
logs\*.txt
```

Reports can include unmatched files, ambiguous sidecar matches, suspicious dates, metadata conflicts, metadata write/verification problems and other review items.

## Build from source

Requirements:

- Go
- GCC/MinGW on Windows for the desktop build
- ExifTool for metadata-writing workflows

Run tests:

```powershell
go test ./...
```

Build the Windows application:

```powershell
.\scripts\build-windows.ps1
```

The build script writes:

```text
dist\GoogleTakeoutFixer.exe
```

## Project background

This project began as a fork of [`feloex/GoogleTakeoutFixer`](https://github.com/feloex/GoogleTakeoutFixer) and has since been expanded with its current split-ZIP indexing, deterministic matching, resumable batch processing, SSD staging, metadata verification/fallback, reporting, GUI workflow and Motion Photo migration pipeline.

Metadata processing uses [ExifTool](https://exiftool.org/) by Phil Harvey. Optional Motion Photo rebuilding uses [MotionPhoto2](https://github.com/PetrVys/MotionPhoto2).

## Licence

This project is distributed under the **GNU General Public License v3.0 or later**. See [`LICENSE`](LICENSE).
