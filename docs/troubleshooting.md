# Troubleshooting

## ExifTool not found

Symptoms:

- GUI dependency warning mentions ExifTool
- CLI exits before processing with an ExifTool error

Fix:

1. Use a bundled release build, which includes ExifTool.
2. Or install ExifTool separately and ensure it is on `PATH`.

Quick checks:

```sh
exiftool -ver
```

## Input and output folder must be different

The app rejects unsafe path layouts where:

- input equals output
- output is inside input
- input is inside output

Choose a sibling output directory such as `Google Photos Fixed`.

## Run resumed instead of rewriting files

That is expected when `OUTPUT/.gtf/state.jsonl` already contains successful records for the same source file and output path.

If you intentionally want a fresh run:

1. Delete or archive `OUTPUT/.gtf/`
2. Or choose a new output folder

## Metadata verification failed

Common causes:

- media format supports only a subset of tags
- file is read-only
- external tools or sync clients are holding the file open
- embedded metadata conflicts with what the chosen policy tries to write

Recommended steps:

1. Re-run with `--dry-run` to inspect the report first.
2. Check `OUTPUT/.gtf/reports/latest.txt`.
3. Inspect the file manually with:

```sh
exiftool -time:all -gps:all -a -G1 "path/to/file"
```

## GUI opens but build fails locally

The desktop app uses Fyne with CGO.

Windows:

- install Go
- install a C compiler such as MinGW/WinLibs
- run `powershell -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1`

Linux:

- install `gcc` and the required X11/Wayland development libraries

macOS:

- install Xcode command line tools

## Corrupt state file warnings

Corrupt JSONL lines are skipped on load so a partial or interrupted run does not destroy the whole state file.

If the warnings keep growing:

1. back up `OUTPUT/.gtf/state.jsonl`
2. inspect the invalid lines
3. delete the state file if you want to rebuild it from scratch
