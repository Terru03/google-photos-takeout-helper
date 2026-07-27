# Google Photos Takeout Helper

<p align="center">
    <img src="images/GoogleTakeoutFixer.png" alt="GoogleTakeoutFixer logo" width="200"/>
</p>

GoogleTakeoutFixer is a local Google Photos Takeout repair and migration app for large photo libraries. It is the maintained `Terru03/google-photos-takeout-helper` project and keeps the Windows executable name `GoogleTakeoutFixer.exe`.

It turns messy Google Photos Takeout exports into a cleaner folder library with restored metadata, deterministic media-to-JSON matching, resumable processing, audit reports, verification, and safer workflows for very large split-ZIP exports.

## Safety

- Original ZIP files are never moved, modified, or deleted.
- Output is written only to `--output`.
- Temporary extraction happens only under the selected work folder or work pool.
- Optional SSD staging writes one ZIP to fast storage, then commits it to final output.
- Batch ZIP mode processes one ZIP at a time by default.
- Review items stay in reports instead of being hidden.

## Main Features

- Match media files to Google Takeout JSON sidecars across all selected split ZIPs.
- Write metadata into photos and videos with ExifTool.
- Verify written metadata with `--verify`.
- Deduplicate exact copies safely.
- Restore `.MOV` extensions when embedded metadata says QuickTime.
- Report unmatched, ambiguous, suspicious date, conflict, and metadata write/verify records.
- Resume huge Takeout ZIP batches from `OUTPUT\.gtf\batch\manifest.jsonl`.
- Run motion-photo merge once, after ZIP work, with timeout and resume.

## Install

1. Download a release from this repository.
2. Extract the archive.
3. Run `GoogleTakeoutFixer.exe`.

Windows SmartScreen may warn on unsigned community builds. Verify release checksums when provided.

Optional Motion Photo rebuilding needs [MotionPhoto2](https://github.com/PetrVys/MotionPhoto2) installed separately or placed beside the app binary.

## Desktop Use

Run `GoogleTakeoutFixer.exe` with no arguments to open the desktop app.

In Batch ZIP mode, add one or more ZIP source folders, choose output, add one or more temporary work folders, run preflight, then start processing.

## CLI Use

The same executable also supports CLI mode when flags are passed.

Extracted folder:

```powershell
.\GoogleTakeoutFixer.exe --input "D:\Takeout\Google Photos" --output "B:\Google_Photos_Final" --verify
```

Audit only:

```powershell
.\GoogleTakeoutFixer.exe --profile audit-only --input "D:\Takeout\Google Photos" --output "B:\Google_Photos_Final"
```

Recommended safe mode:

```powershell
.\GoogleTakeoutFixer.exe --profile recommended-safe --input "D:\Takeout\Google Photos" --output "B:\Google_Photos_Final"
```

## Huge ZIP Batch Mode

Single work folder:

```powershell
.\GoogleTakeoutFixer.exe --batch-zips --zip-root "D:\Takeout Zips" --work "C:\Takeout_Incoming" --output "B:\Google_Photos_Final" --verify
```

Multiple work folders:

```powershell
.\GoogleTakeoutFixer.exe --batch-zips --zip-root "D:\Takeout Zips" --work "C:\Takeout_Incoming" --work "E:\Takeout_Incoming" --output "B:\Google_Photos_Final" --verify
```

SSD staging output:

```powershell
.\GoogleTakeoutFixer.exe --batch-zips --zip-root "D:\Takeout Zips" --work "C:\Takeout_Work" --staging-output "C:\Takeout_Staging" --output "B:\Google_Photos_Final" --timeline-only --verify
```

One ZIP debug run:

```powershell
.\GoogleTakeoutFixer.exe --one-zip "D:\Takeout Zips\takeout-20260624T210653Z-3-001.zip" --work "C:\Takeout_Work" --staging-output "C:\Takeout_Staging" --output "B:\Google_Photos_Final" --timeline-only --verify
```

Alternate work pool syntax:

```powershell
.\GoogleTakeoutFixer.exe --batch-zips --zip-root "D:\Takeout Zips" --work-pool "C:\Takeout_Incoming;E:\Takeout_Incoming" --output "B:\Google_Photos_Final" --verify
```

Preflight only:

```powershell
.\GoogleTakeoutFixer.exe --batch-zips --preflight-only --zip-root "D:\Takeout Zips" --work "C:\Takeout_Incoming" --work "E:\Takeout_Incoming" --output "B:\Google_Photos_Final"
```

Multiple work folders help with free space. `--staging-output` gives the larger speed gain when final output is an HDD: metadata work runs on SSD, then completed files commit to `--output`.

Batch ZIP mode stays sequential unless a future explicit parallel mode proves output state, manifest writes, reports, duplicate handling, and file conflicts are safe.

Batch mode defaults to timeline-only output. Timeline year and month come from:

1. Google `photoTakenTime.timestamp`, indexed across all selected Takeout ZIPs
2. embedded EXIF/XMP/media creation time
3. a valid date in the media filename
4. the Google timeline folder year, using `Unknown month` when no month is safe
5. file modified time only when no Google timeline year exists

Albums never name a timeline folder. `--albums-separate` writes albums below `OUTPUT\Albums\`.

## Motion Photo Merge Pass

Phase 1 keeps both still images and companion `.MOV`/`.MP4` files. It never calls MotionPhoto2.

After all ZIPs finish, run:

```powershell
.\GoogleTakeoutFixer.exe --merge-motion-pass --library "B:\Google_Photos_Final" --motionphoto2 "C:\Tools\motionphoto2.exe"
```

Each pair has a two-minute default timeout. One bad pair does not stop the pass. Successful pairs skip on rerun. Retry failed or timed-out pairs with:

```powershell
.\GoogleTakeoutFixer.exe --merge-motion-pass --library "B:\Google_Photos_Final" --motionphoto2 "C:\Tools\motionphoto2.exe" --retry-failed-motion
```

Report:

```text
OUTPUT\.gtf\reports\motion-merge-report.json
```

## Batch Status

Batch ZIP status is written to:

```text
OUTPUT\.gtf\batch\manifest.jsonl
```

Statuses:

- `completed`: ZIP processed cleanly.
- `completed_with_review`: ZIP processed and report has review items.
- `failed`: extraction, folder detection, crash, panic, disk/write, or fatal processing error.
- `interrupted`: previous run stopped before ZIP finished.

Completed and `completed_with_review` ZIPs are skipped on rerun. Use `--reprocess` only when you want to process them again.

Review items do not fail the whole ZIP:

- unmatched files
- ambiguous matches
- suspicious dates
- metadata conflicts
- metadata write or report row errors recorded in the report

Fatal failures still fail the ZIP:

- ZIP extraction failure
- process crash, panic, or unhandled fatal error
- cannot locate `Takeout\Google Photos` or `Google Photos`
- disk or write failure

After `completed` or `completed_with_review`, the selected `gtf-zip-*` temp folder is deleted. On true failure, temp folders are kept only when `--keep-temp-on-error` is used.

## Path Safety

ZIP extraction sanitizes each path component for Windows:

- rejects absolute paths, drive names, `.`, `..`, and traversal
- trims trailing spaces and dots
- replaces `< > : " | ? *` and control characters with `_`
- protects reserved names such as `CON`, `PRN`, `AUX`, `NUL`, `COM1` to `COM9`, and `LPT1` to `LPT9`
- keeps the relative `Takeout\Google Photos\...` structure when safe

## Reports

Runtime files are under `OUTPUT\.gtf\`:

- `state.jsonl`: resumable processing state
- `batch\manifest.jsonl`: ZIP batch status and selected work root/temp folder
- `batch\preflight_latest.txt`: latest preflight summary
- `reports\latest.txt`: human report
- `reports\latest.json`: machine-readable report
- `reports\suspicious_dates.csv`: suspicious date rows
- `logs\*.txt`: run logs
- `reports\motion-merge-report.json`: separate motion merge results

## Development

```powershell
# Run tests
go test ./...

# Build one Windows executable
.\scripts\build-windows.ps1
```

The build script cleans `dist` and writes only:

```text
dist\GoogleTakeoutFixer.exe
```

## Credits

This project began as a fork of [`feloex/GoogleTakeoutFixer`](https://github.com/feloex/GoogleTakeoutFixer). It now carries its own matching, deduplication, state, reporting, verification, batch ZIP, GUI, and migration workflow code.

Metadata work uses [ExifTool](https://exiftool.org/) by Phil Harvey.

## Disclaimer

Always keep a backup of your original Google Takeout files and review reports before deleting any source archive.
