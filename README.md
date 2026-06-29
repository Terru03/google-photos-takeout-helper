# GoogleTakeoutFixer

<p align="center">
    <img src="images/GoogleTakeoutFixer.png" alt="GoogleTakeoutFixer logo" width="200"/>
</p>

GoogleTakeoutFixer is a local Google Photos Takeout repair and migration tool for large photo libraries.

It helps turn a messy Google Photos Takeout export into a cleaner folder library with restored metadata, deterministic media-to-JSON matching, resumable processing, audit reports, verification, and safer workflows for very large split-ZIP exports.

The app is built for people who want to keep, migrate, or import their Google Photos archive without losing dates, locations, album structure, live/motion photo relationships, or the ability to check what happened to each file.

## Project status

This repository started as a fork of `feloex/GoogleTakeoutFixer`. It is now my maintained version with substantial additional work around safer matching, auditability, huge Takeout handling, resumable state, verification, and migration-oriented workflows.

Credit remains with the original project and other Google Takeout repair tools that helped shape the direction. This version keeps its own implementation choices, state model, reporting model, and workflow documentation.

## Why Google Takeout needs repair

Google Photos Takeout exports are useful, but they are often difficult to use directly:

- dates and locations can be split between media files and JSON sidecars;
- media files and JSON files can have awkward or truncated names;
- edited files, duplicate suffixes, and live-photo partner files can be hard to match safely;
- the same media can appear in both year folders and album folders;
- very large exports can be split across many ZIP files;
- users need proof that files were matched, skipped, deduplicated, or failed.

GoogleTakeoutFixer focuses on safe repair rather than silent guesses. Ambiguous files are reported instead of being matched blindly.

## Safe workflow first

Recommended workflow:

1. Keep the original Google Takeout ZIP files.
2. Run **Audit Only** first.
3. Check the report under `OUTPUT/.gtf/reports/`.
4. Run **Recommended Safe Mode**.
5. Review unmatched, ambiguous, suspicious, and failed verification files.
6. Only use `--delete-source` if the run is clean and you already have backups.

Deleting source files is an advanced option. It should not be part of a normal first run.

## What is safe by default

- The app works locally on your machine.
- It does not upload your photos anywhere.
- Original ZIP files are not moved or deleted.
- Batch ZIP mode extracts and processes one ZIP at a time.
- Reports and resumable state are written under the output `.gtf` folder.
- Destructive cleanup requires explicit options and a clean run.

## Features

| Feature | What it does | Why it matters |
|---|---|---|
| Metadata writing | Writes Google Takeout JSON date, GPS, and related metadata into media files. | Restores sort order and location data for local libraries. |
| Deterministic sidecar matching | Matches media to JSON using exact names, JSON titles, duplicate suffix handling, edited variants, long-name handling, and live/motion partner logic. | Avoids the dangerous “first similar JSON wins” behaviour. |
| Exact duplicate dedupe | Reuses the first output copy for exact duplicate media across year and album folders. | Reduces wasted storage without guessing based on names alone. |
| Resume support | Stores append-only state under `OUTPUT/.gtf/state.jsonl`. | Interrupted runs can continue instead of starting over. |
| Audit reports | Writes human-readable and machine-readable reports. | You can review matched, unmatched, ambiguous, duplicate, conflict, and verification results. |
| Verification | Reads metadata back with ExifTool when requested. | Confirms what actually landed in the file. |
| Huge ZIP batch mode | Processes split Takeout ZIP exports one at a time. | Makes multi-terabyte exports more practical and safer to resume. |
| MotionPhoto2 support | Optionally rebuilds eligible still+video pairs into Windows-viewable Samsung/Google Motion Photos. | Helps preserve live/motion photo behaviour in local folders. |
| Recommended Safe Mode | Applies conservative settings for a normal repair run. | Gives users a safer starting point. |
| Dry run / Audit Only | Plans and reports without writing repaired media. | Lets users inspect problems before changing files. |
| Suspicious date report | Lists missing, old, future, conflicting, or guessed timestamps. | Makes date problems visible instead of hiding them. |

## Preview

<p align="center">
    <img src="images/GTFWindow-v1.3.0.png" alt="GoogleTakeoutFixer window" width="460"/>
</p>

## Installation

1. Download a bundled release of GoogleTakeoutFixer from this repository's releases.
2. Choose the build that matches your operating system.
3. Extract the downloaded archive.
4. Run the executable file.

Bundled release builds include ExifTool. Development builds may require ExifTool to be installed separately and available on `PATH`.

> [!NOTE]
> Creating Windows Motion Photos is optional and requires [MotionPhoto2](https://github.com/PetrVys/MotionPhoto2) to be installed separately or placed beside the app binary.

> [!IMPORTANT]
> Windows SmartScreen may warn about unsigned community builds. Use releases from this repository, check release notes, and verify checksums if they are provided.

## Preparing a Google Takeout export

1. Go to [takeout.google.com](https://takeout.google.com/) and click **Deselect all**.
2. Select **Google Photos**.
3. Click **Next Step**.
4. Choose how to receive the archive.
5. For large libraries, select the largest archive size available to reduce the number of split files.
6. Download all ZIP files before starting a batch run.

For smaller exports, extract the archives and select the folder named `Google Photos` as the input folder. This folder should contain subfolders like `Photos from 2024` and album folders.

For large split-ZIP exports, use **Huge ZIP Batch** mode instead of extracting everything at once.

## Desktop workflow

1. Click **Select Google Takeout folder** and choose the extracted `Google Photos` folder.
2. Let the app suggest a sibling output folder, or choose your own output folder.
3. Start with **Audit Only** or **Recommended Safe Mode**.
4. Review the latest audit report after the run.
5. Open the output folder and reports from the GUI when processing finishes.

Common options:

- **Write metadata**: writes metadata from JSON files into media files.
- **Use symlinks for albums**: creates file links instead of duplicating album media.
- **Ignore album folders**: processes only year folders.
- **Create month subfolders**: creates month folders like `1 - January` through `12 - December`.
- **Flatten output structure**: writes all files directly into the output folder.
- **Create Windows Motion Photos**: runs MotionPhoto2 after processing.
- **Restore .MOV file extension**: restores `.MOV` when metadata indicates Apple QuickTime.
- **Delete input folder after clean run**: advanced cleanup option. Use only after a clean run and backup review.

## CLI usage

```sh
./GoogleTakeoutFixer --input "/path/to/takeout/Google Photos/" --output "/path/to/output/folder/" --verify
```

Useful flags:

- `--input "PATH"`: path to extracted Google Photos Takeout folder;
- `--output "PATH"`: path to repaired output folder;
- `--dry-run`: plan the run and emit reports without writing repaired media;
- `--verify`: read metadata back with ExifTool after writing;
- `--symlink`: use symlinks inside albums instead of duplicating media;
- `--skip-metadata`: skip writing metadata to files;
- `--ignore-albums`: ignore album folders and process only year folders;
- `--month-subfolders`: create month subfolders;
- `--flatten`: put all files directly in the output folder;
- `--motion-photos`: rebuild eligible image/video pairs with MotionPhoto2;
- `--keep-live-video`: keep standalone live-video files after MotionPhoto2 embeds them;
- `--delete-source`: delete the original input folder only after a fully clean run;
- `--restore-mov`: restore `.MOV` extension when metadata indicates Apple QuickTime;
- `--no-deduplicate`: keep exact duplicate files instead of linking or reusing them;
- `--conflict-policy prefer-json|prefer-embedded|merge`: choose metadata conflict behaviour;
- `--version`: show version;
- `--help`: show help.

## Huge split-ZIP exports

Use Huge ZIP Batch mode when your Google Takeout export is split across many ZIP files or is too large to extract all at once.

You need three separate locations:

- ZIP source folder or folders, such as `D:\Takeout_Zips`;
- temporary work folder, such as `C:\GTF_Work`;
- final output folder, such as `B:\Google_Photos_Final`.

Basic preflight command:

```powershell
.\gtf-cli.exe --batch-zips --preflight-only --zip-root "D:\Takeout_Zips" --work "C:\GTF_Work" --output "B:\Google_Photos_Final"
```

Batch mode safety rules:

- original ZIP files are never moved or deleted;
- final output is never zipped back up;
- ZIP source roots, temp work folder, and final output folder must not overlap;
- failed or interrupted ZIPs are retried on the next run;
- completed ZIPs are skipped unless `--reprocess` is set.

Full guide: [Huge Takeout workflow](./docs/huge-takeout-workflow.md)

## Migration workflows

GoogleTakeoutFixer prepares a repaired local library. It does not directly upload to other services.

Useful workflows:

- **Immich-ready local library**: repair dates, GPS data, duplicates, and reports before importing with Immich tools.
- **Synology or normal folder library**: produce a cleaner folder structure for a NAS or external drive.
- **Archive-safe workflow**: keep original ZIPs, repaired output, and `.gtf` reports together for long-term verification.
- **Lightroom or digiKam workflow**: future XMP sidecar support could make non-destructive metadata workflows easier.

Full guide: [Migration workflows](./docs/migration-workflows.md)

## Reports and runtime files

During each run, the tool writes resumable state and audit artifacts under `OUTPUT/.gtf/`:

- `state.jsonl`: append-only processing state used for resumable and idempotent runs;
- `batch/manifest.jsonl`: batch ZIP status and resume manifest;
- `batch/preflight_latest.txt`: latest preflight report;
- `reports/latest.txt`: human-readable audit summary;
- `reports/latest.json`: detailed machine-readable report;
- `reports/suspicious_dates.csv`: files with missing, old, future, conflicting, or guessed timestamps;
- `logs/*.txt`: per-run logs written beside the repaired library.

## Development

This project uses [Go](https://go.dev/) and [Fyne](https://fyne.io/) for the desktop GUI.

```sh
# Clone this repo
git clone https://github.com/Terru03/google-photos-takeout-helper.git
cd google-photos-takeout-helper

# Run the hybrid desktop app / CLI entrypoint
go run ./cmd

# Build a standalone CLI binary
go build ./cmd/gtf-cli

# Run tests
go test ./internal/fixer ./internal/cli

# Run lint on the core packages
golangci-lint run ./internal/fixer/... ./internal/cli/...
```

Windows build:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1
```

To run the GUI locally, make sure the required Fyne dependencies are installed. See the [Fyne prerequisites](https://docs.fyne.io/started/quick/#prerequisites).

## Docs

- [Architecture](./docs/architecture.md)
- [Troubleshooting](./docs/troubleshooting.md)
- [Huge Takeout workflow](./docs/huge-takeout-workflow.md)
- [Migration workflows](./docs/migration-workflows.md)
- [Versioning](./docs/versioning.md)

## Roadmap

Planned or possible improvements:

- clearer Immich-ready output profile;
- XMP sidecar mode for non-destructive metadata workflows;
- stronger final migration proof report;
- GUI review page for unmatched and ambiguous files;
- signed releases or release checksums;
- more test fixtures for unusual Google Takeout filenames, duplicates, edited files, live photos, and split ZIPs.

## Credits

This project modifies metadata using [ExifTool](https://exiftool.org/) by Phil Harvey. ExifTool is licensed under the Perl Artistic license or the GNU General Public License.

Optional Motion Photo rebuilding uses [MotionPhoto2](https://github.com/PetrVys/MotionPhoto2) when configured by the user.

This repository began as a fork of `feloex/GoogleTakeoutFixer`. Matching behaviour and product direction were also informed by other Google Takeout repair tools, especially `gophix`, while this codebase maintains its own implementation and state/reporting model.

## Disclaimer

Not affiliated with Google LLC.
