# Troubleshooting

This page covers common problems when repairing Google Photos Takeout exports with GoogleTakeoutFixer.

Start with the safest path:

1. Keep the original Takeout ZIPs.
2. Run **Audit Only** or `--dry-run` first.
3. Read `OUTPUT/.gtf/reports/latest.txt`.
4. Only run a write mode after the report looks reasonable.

## ExifTool not found

Symptoms:

- GUI dependency warning mentions ExifTool.
- CLI exits before processing with an ExifTool error.
- Metadata writing or verification is skipped or fails immediately.

Fix:

1. Use a bundled release build, which includes ExifTool.
2. Or install ExifTool separately and make sure it is available on `PATH`.
3. Restart the terminal or desktop app after changing `PATH`.

Quick check:

```sh
exiftool -ver
```

## MotionPhoto2 not found

Symptoms:

- Motion Photo rebuilding does not run.
- The report shows MotionPhoto2-related failures.
- The normal Takeout repair works, but Windows-viewable Motion Photos are not created.

Fix:

1. Install MotionPhoto2 separately.
2. Place the MotionPhoto2 executable beside the app binary or make it available through the expected path.
3. Re-run with Motion Photo support enabled.

MotionPhoto2 is optional. Leave Motion Photo rebuilding disabled if you only need normal metadata repair.

## Windows SmartScreen warning

Windows may warn when running unsigned community builds.

Recommended checks:

- download releases only from this repository;
- check release notes;
- verify checksums if they are attached to a release;
- keep the original Takeout ZIPs before running any repair tool.

If you trust the downloaded release, click **More info** and then **Run anyway**.

## Input and output folder must be different

The app rejects unsafe path layouts where:

- input equals output;
- output is inside input;
- input is inside output.

Choose a sibling output directory such as:

```text
Google Photos
Google Photos Fixed
```

For batch ZIP mode, ZIP roots, temp work folder, and final output folder must also be separate.

## Permission denied files

Common causes:

- files are read-only;
- files are open in another app;
- cloud sync software is locking the file;
- antivirus software is scanning the file;
- the output drive has restricted permissions.

Recommended steps:

1. Close photo viewers, editors, and file sync tools.
2. Try a different output folder.
3. Run the app from a normal user folder, not a protected system folder.
4. Re-run after checking `OUTPUT/.gtf/reports/latest.txt`.

## Locked files

Symptoms:

- metadata writing fails for a small number of files;
- verification fails even though matching succeeded;
- the same file works after a restart.

Fix:

1. Close any app that may be previewing or indexing the file.
2. Pause OneDrive, Google Drive, Dropbox, or similar sync clients.
3. Restart Windows if the lock is unclear.
4. Re-run. The resumable state should avoid repeating completed work unnecessarily.

## Run resumed instead of rewriting files

That is expected when `OUTPUT/.gtf/state.jsonl` already contains successful records for the same source file and output path.

If you intentionally want a fresh run:

1. Delete or archive `OUTPUT/.gtf/`.
2. Or choose a new output folder.

Do not delete `.gtf` during a run.

## Interrupted runs

If the app stops, the computer sleeps, or a drive disconnects, check the report and logs under `OUTPUT/.gtf/`.

For normal folder mode:

- restart the same command or GUI run with the same input and output;
- already completed files should be skipped where possible;
- failed files should be reported again.

For batch ZIP mode:

- restart with the same ZIP roots, work folder, and output folder;
- completed ZIPs are skipped using `OUTPUT/.gtf/batch/manifest.jsonl`;
- failed or interrupted ZIPs are retried on the next run;
- use `--reprocess` only if you intentionally want to process completed ZIPs again.

## Suspicious dates

The suspicious date report lists files with missing, old, future, conflicting, or guessed timestamps.

Report location:

```text
OUTPUT/.gtf/reports/suspicious_dates.csv
```

Recommended steps:

1. Open the CSV and inspect a few examples.
2. Compare the media file with its JSON sidecar.
3. Check whether the date came from JSON, embedded metadata, or a fallback.
4. Do not delete originals until suspicious dates are understood.

## Metadata verification failed

Common causes:

- media format supports only a subset of tags;
- file is read-only;
- external tools or sync clients are holding the file open;
- embedded metadata conflicts with the chosen conflict policy;
- ExifTool wrote a tag to a different metadata group than expected.

Recommended steps:

1. Re-run with `--dry-run` to inspect the plan first.
2. Check `OUTPUT/.gtf/reports/latest.txt`.
3. Inspect the file manually with:

```sh
exiftool -time:all -gps:all -a -G1 "path/to/file"
```

A verification failure does not always mean the file is unusable. It means the app could not prove the expected metadata landed exactly as requested.

## Huge ZIP temp folder problems

Symptoms:

- preflight reports not enough free space;
- extraction fails part-way through a ZIP;
- temp folder and output folder overlap;
- reruns keep using stale temporary files.

Fix:

1. Use a temp folder on a fast drive with enough space for one full ZIP plus margin.
2. Keep ZIP roots, temp work folder, and final output folder separate.
3. Use `--preflight-only` before the real batch run.
4. Use `--keep-temp-on-error` if you need to inspect a failed extraction.
5. Clean the temp folder only when no run is active.

Example:

```powershell
.\gtf-cli.exe --batch-zips --preflight-only --zip-root "D:\Takeout_Zips" --work "C:\GTF_Work" --output "B:\Google_Photos_Final"
```

## Output folder inside input folder mistakes

Do not write repaired output into the source Takeout folder.

Bad:

```text
Input:  D:\Takeout\Google Photos
Output: D:\Takeout\Google Photos\Fixed
```

Good:

```text
Input:  D:\Takeout\Google Photos
Output: D:\Takeout\Google Photos Fixed
```

For huge ZIP mode, also avoid putting the temp work folder inside the output folder.

## GUI opens but local build fails

The desktop app uses Fyne with CGO.

Windows:

- install Go;
- install a C compiler such as MinGW or WinLibs;
- run `powershell -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1`.

Linux:

- install `gcc` and the required X11/Wayland development libraries.

macOS:

- install Xcode command line tools.

## Corrupt state file warnings

Corrupt JSONL lines are skipped on load so a partial or interrupted run does not destroy the whole state file.

If the warnings keep growing:

1. back up `OUTPUT/.gtf/state.jsonl`;
2. inspect the invalid lines;
3. delete the state file only if you want to rebuild state from scratch.
