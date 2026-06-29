# Huge Takeout workflow

Use this workflow when a Google Photos Takeout export is too large to extract all at once, or when the export is split across many ZIP files.

The goal is to repair the library safely while keeping the original ZIP files untouched.

## When to use this mode

Use Huge ZIP Batch mode if:

- your export is split across many ZIP files;
- the full extracted library would not fit on your temp drive;
- you want resumable processing across multiple drives;
- you want one final repaired folder library instead of many temporary extracted folders.

For small exports, it is usually simpler to extract the archives first and use normal folder mode.

## Required folders

Keep these locations separate:

| Location | Purpose | Example |
|---|---|---|
| ZIP source root | Where the original Takeout ZIPs are stored | `D:\Takeout_Zips` |
| Work folder | Temporary extraction folder for one ZIP at a time | `C:\GTF_Work` |
| Final output folder | Repaired photo library | `B:\Google_Photos_Final` |

Do not put the work folder inside the output folder. Do not put the output folder inside a ZIP source folder.

## Safety rules

- Original ZIP files are never moved or deleted.
- Final output is never zipped back up.
- Each ZIP is extracted and processed separately.
- After a clean ZIP run, only that ZIP's temporary extracted folder is deleted.
- Failed or interrupted ZIPs are retried on the next run.
- Completed ZIPs with the same path, size, and modified time are skipped unless `--reprocess` is set.
- Resume state and reports stay under `OUTPUT/.gtf/`.

## Recommended workflow

### 1. Preflight only

Run preflight before extracting or repairing anything.

```powershell
.\gtf-cli.exe --batch-zips --preflight-only --zip-root "D:\Takeout_Zips" --work "C:\GTF_Work" --output "B:\Google_Photos_Final"
```

If your ZIPs are split across multiple drives or folders, repeat `--zip-root`:

```powershell
.\gtf-cli.exe --batch-zips --preflight-only --zip-root "D:\Takeout_Zips" --zip-root "F:\Takeout_Zips" --work "C:\GTF_Work" --output "B:\Google_Photos_Final"
```

Review:

```text
OUTPUT\.gtf\batch\preflight_latest.txt
```

### 2. Dry run

A dry run extracts one ZIP at a time and writes reports without writing repaired media.

```powershell
.\gtf-cli.exe --batch-zips --zip-root "D:\Takeout_Zips" --work "C:\GTF_Work" --output "B:\Google_Photos_Final" --dry-run
```

Use this to check matching quality, suspicious dates, and folder layout before a real run.

### 3. Real batch run

```powershell
.\gtf-cli.exe --batch-zips --zip-root "D:\Takeout_Zips" --work "C:\GTF_Work" --output "B:\Google_Photos_Final" --verify
```

Use `--verify` when you want the tool to read metadata back with ExifTool after writing.

### 4. Resume after interruption

Use the same command again:

```powershell
.\gtf-cli.exe --batch-zips --zip-root "D:\Takeout_Zips" --work "C:\GTF_Work" --output "B:\Google_Photos_Final" --verify
```

Completed ZIPs are skipped using:

```text
OUTPUT\.gtf\batch\manifest.jsonl
```

### 5. Motion Photos

If MotionPhoto2 is installed and configured, you can enable Motion Photo rebuilding:

```powershell
.\gtf-cli.exe --batch-zips --zip-root "D:\Takeout_Zips" --work "C:\GTF_Work" --output "B:\Google_Photos_Final" --motion-photos --keep-live-video --verify
```

In batch mode, keeping live-video files is safer because it preserves the original partner video output even after MotionPhoto2 embeds a playable Motion Photo.

## Useful flags

| Flag | Use |
|---|---|
| `--batch-zips` | Enables split-ZIP processing. |
| `--zip-root "PATH"` | Adds a ZIP file or folder to scan for Takeout ZIPs. Can be repeated. |
| `--work "PATH"` | Temporary extraction folder. |
| `--output "PATH"` | Final repaired library folder. |
| `--preflight-only` | Checks ZIPs, paths, and space risks without extracting. |
| `--dry-run` | Extracts and audits without writing repaired media. |
| `--verify` | Reads metadata back after writing. |
| `--ask-on-ambiguous` | Asks before continuing when drive/path choices are unclear. |
| `--keep-temp-on-error` | Keeps extracted temp files after a failure for review. |
| `--reprocess` | Processes ZIPs again even if the manifest says they completed. |
| `--motion-photos` | Runs MotionPhoto2 after the repair pipeline. |
| `--keep-live-video` | Keeps standalone live-video files after Motion Photo embedding. |

## Report locations

Batch mode writes useful files under the final output folder:

```text
OUTPUT\.gtf\state.jsonl
OUTPUT\.gtf\batch\manifest.jsonl
OUTPUT\.gtf\batch\preflight_latest.txt
OUTPUT\.gtf\reports\latest.txt
OUTPUT\.gtf\reports\latest.json
OUTPUT\.gtf\reports\suspicious_dates.csv
OUTPUT\.gtf\logs\
```

Keep `.gtf` with the repaired library. It is the proof of what happened during the migration.

## Common mistakes

### Work folder overlaps with output

Bad:

```text
Work:   B:\Google_Photos_Final\work
Output: B:\Google_Photos_Final
```

Good:

```text
Work:   C:\GTF_Work
Output: B:\Google_Photos_Final
```

### Output goes into a ZIP source folder

Bad:

```text
ZIPs:   D:\Takeout_Zips
Output: D:\Takeout_Zips\Fixed
```

Good:

```text
ZIPs:   D:\Takeout_Zips
Output: B:\Google_Photos_Final
```

### Running delete-source too early

Do not use destructive cleanup until:

- the run is clean;
- reports have been reviewed;
- suspicious dates and verification failures are understood;
- original ZIPs are backed up.

For huge Takeout archives, keeping the original ZIPs is usually the better long-term safety choice.
