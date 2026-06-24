# GoogleTakeoutFixer

<p align="center">
    <img src="images/GoogleTakeoutFixer.png" alt="drawing" width="200"/>
</p>

A tool to easily clean and organize Google Photos Takeout exports.

## The Issue
When you download your files from Google's "Google Photos" service through "Google Takeout", the exported data is **inconsistently organized and often fragmented/broken.**
This can lead to problems:
- Files cannot be reliably sorted or grouped by date or location
- The export contains unnecessary files and a cluttered folder structure
- Your takeout having a big file size due to duplicated media and unnecessary JSON files

## Solution
GoogleTakeoutFixer solves these issues by:
- **Writing metadata** into photos and videos using Google Takeout JSON data.
- **Matching media to sidecars deterministically**, including duplicate suffixes, edited variants, long-name truncation, and live/motion-photo partner files.
- **Optionally rebuilding Windows-viewable motion photos** with MotionPhoto2 by converting eligible image/video pairs into Samsung/Google Motion Photos.
- **Deduplicating exact duplicate media** across year and album folders by reusing the first output copy instead of storing duplicates again.
- **Resuming safely** with a persisted state file so failed runs can continue instead of restarting from scratch.
- **Producing audit reports** with matched, unmatched, ambiguous, duplicate, conflict, and verification results.
- **Verifying metadata after write** when requested, so the tool can prove what actually landed in the file.
- **Processing huge split Takeout ZIP exports one ZIP at a time** without re-zipping final output or deleting original ZIP files.

## Preview
<p align="center">
    <img src="images/GTFWindow-v1.3.0.png" alt="GoogleTakeoutFixer Window" width="460"/>
</p>

## Tutorial
### 1. Preparation
To use GoogleTakeoutFixer, you must have downloaded your photos from Google Takeout and extracted them. Follow these steps:

1. Go to [takeout.google.com](https://takeout.google.com/) and click "Deselect all".

    <img src="images/DeselectAllTakeout.png" alt="Google Takeout deselect button" width="400"/>
2. Scroll down and select "Google Photos".

    <img src="images/TakeoutPhotosSelect.png" alt="Google Takeout Selected" width="400"/>
3. Scroll down to the bottom and click "Next Step".

4. In the "Transfer to" section, choose how you'd like to receive your download link. I recommend choosing email. For "File size", select 50 GB for easier handling.

    <img src="images/CreateExportTakeout.png" alt="Create Export options" width=300>
5. Click "Create export" and follow the instructions.

> [!NOTE]
> - If your Google Takeout is small enough, extract all the archives and move the extracted files into a single folder. This ensures that GoogleTakeoutFixer can process all your files correctly.
> - If your Google Takeout is many terabytes and split across many 50 GB ZIP files, use **Batch Takeout ZIP Mode** instead. It extracts and processes one ZIP at a time.
> - Select the folder named "Google Photos" as your input folder. This folder should contain subfolders like "Photos from (year)" and folders with the names of your albums. Do not select a parent folder of "Google Photos".

### 2. Installation
1. Download a bundled release of GoogleTakeoutFixer from this repository's releases. Choose the version that matches your operating system.
2. Extract the downloaded archive.
3. Run the executable file.

> [!NOTE]
> Creating Windows Motion Photos is optional and requires [MotionPhoto2](https://github.com/PetrVys/MotionPhoto2) to be installed separately or placed beside the app binary.

> [!IMPORTANT]
> When running the executable, a window about security can pop-up if you are using Windows. **Click "more info" and "run anyway"**.

### 3. Using GoogleTakeoutFixer
1. Click **"Select Google Takeout folder"** and choose the folder where you extracted your Google Takeout photos. This folder is named something like "Google Photos".
2. Let the app suggest a sibling output folder automatically, or click **"Select output folder"** and choose your own.
3. Choose the options that you want to apply:
    - **"Write metadata"**: Writes metadata from JSON files into the media files. May not be necessary.
    - **"Use symlinks for albums"**: Creates file links instead of duplicating files for albums.
    - **"Ignore album folders"**: Ignores album folders and only processes year folders.
    - **"Create month subfolders"**: Creates month subfolders like `1 - January` through `12 - December` inside of the output folders.
    - **"Flatten output structure"**: Puts all files directly in the output folder.
    - **"Create Windows Motion Photos (MotionPhoto2)"**: Runs MotionPhoto2 after processing to rebuild eligible still+video pairs into Samsung/Google Motion Photos that the Windows Photos app can play.
    - **"Delete input folder after clean run"**: Deletes the original input folder only after a fully clean run with zero unmatched, ambiguous, or error records.
    - **"Restore .MOV file extension"**: Restores .MOV file extension in case the Major Brand EXIF field says "Apple QuickTime (.MOV/QT)" (See #2).
4. For a safe default, click **"Recommended Safe Mode"**. For a no-write trust check, click **"Audit Only"**.
5. Click **"Start processing"** and wait for the process to finish. The time it takes depends on the number of photos and videos you have.

Once the process is complete, you can find your fixed files in the output folder you selected.

You can open the output folder and the latest audit report directly from the GUI after the run finishes.

---

### CLI usage
You can also use GoogleTakeoutFixer through the CLI. Use the following flags:
- `--input "PATH"`: Path to Google takeout directory
- `--output "PATH"`: Path to output directory
- `--symlink`: Use symlinks inside of albums instead of duplicating images
- `--skip-metadata`: Skip writing metadata to files
- `--ignore-albums`: Ignore album folders and only process year folders
- `--month-subfolders`: Create month subfolders like `1 - January` through `12 - December`
- `--flatten`: Flatten the folder structure and put all files directly in the output folder
- `--motion-photos`: Rebuild eligible image/video pairs into Samsung/Google Motion Photos with MotionPhoto2
- `--delete-source`: Delete the original input folder only after a fully clean run with zero unmatched, ambiguous, or error records
- `--restore-mov`: Restore .MOV file extension in case the Major Brand EXIF field says \"Apple QuickTime (.MOV/QT)\" (See #2)
- `--dry-run`: Plan the run and emit reports without writing files
- `--verify`: Read metadata back with ExifTool after writing to validate the result
- `--no-deduplicate`: Keep exact duplicate files instead of linking or reusing them
- `--conflict-policy prefer-json|prefer-embedded|merge`: Choose how JSON and embedded metadata conflicts are resolved
- `--version`: Show version
- `--help`: Show help message

Example usage:
```sh
./GoogleTakeoutFixer --input "/path/to/takeout/Google Photos/" --output "/path/to/output/folder/" --verify
```

Batch Takeout ZIP Mode flags:
- `--batch-zips`: Process Takeout ZIPs one at a time
- `--zip-root "PATH"`: Search a ZIP file or folder for Takeout ZIP files; repeat for multiple drives/folders
- `--work "PATH"`: Temporary extraction/work folder
- `--output "PATH"`: Final unzipped output library
- `--auto-drives`: Scan Windows drives and choose clear defaults
- `--ask-on-ambiguous`: Ask before choosing ambiguous drives or continuing after a problem report
- `--keep-temp-on-error`: Keep extracted temp files when a ZIP fails or needs review
- `--reprocess`: Process ZIPs even if the batch manifest already marks them successful
- `--dry-run`: Scan and write a planned manifest without extracting ZIPs or writing fixed media
- `--verify`: Verify written metadata by reading it back with ExifTool

### Huge Takeout ZIP workflow (Windows)
For a multi-terabyte Google Photos Takeout, keep the original ZIP files on your storage drives and write the final fixed library as normal folders and files.

Example layout:
- ZIP storage drives: `D:\Takeout_Zips`, `E:\Takeout_Zips`, `F:\Takeout_Zips`, or other external HDD folders
- Final output drive: `B:\Google_Photos_Final` on the 8 TB `Backup B` / `B:` drive
- Fast temp work folder: `C:\GTF_Work` if the SSD has enough free space for one ZIP extraction plus margin

Safe dry run:
```powershell
.\gtf-cli.exe --batch-zips --zip-root "D:\Takeout_Zips" --zip-root "F:\Takeout_Zips" --work "C:\GTF_Work" --output "B:\Google_Photos_Final" --dry-run
```

Real run:
```powershell
.\gtf-cli.exe --batch-zips --zip-root "D:\Takeout_Zips" --zip-root "F:\Takeout_Zips" --work "C:\GTF_Work" --output "B:\Google_Photos_Final" --verify
```

Batch mode safety rules:
- Original ZIP files are never moved or deleted.
- Final output is never zipped back up.
- Final output and `.gtf/state.jsonl` stay in the output folder, so dedupe works across multiple ZIPs and later runs.
- ZIP source roots, temp work folder, and final output folder must not overlap.
- After a clean ZIP run, only that ZIP's temporary extracted folder is deleted.
- If the audit report has unmatched, ambiguous, or error records, the batch stops unless you used `--ask-on-ambiguous` and choose to continue.
- Resume uses `OUTPUT\.gtf\batch_manifest.jsonl`; ZIPs marked `success` are skipped unless `--reprocess` is set.

During each run, the tool writes resumable state and audit artifacts under `OUTPUT/.gtf/`:
- `state.jsonl`: append-only processing state used for resumable/idempotent runs
- `batch_manifest.jsonl`: batch ZIP status and resume manifest
- `reports/latest.txt`: human-readable audit summary
- `reports/latest.json`: detailed machine-readable report
- `logs/*.txt`: per-run logs written beside the repaired library instead of the current working directory

If `--motion-photos` is enabled, the audit report also records whether the MotionPhoto2 pass completed successfully.

GUI preferences are saved in your user config directory as `GoogleTakeoutFixer/config.json`.

You might have to give the executable permissions to run on Linux and macOS using `chmod +x GoogleTakeoutFixer` before you can run it through the terminal.

## Development
### Setup
This project uses [Go](https://go.dev/) as the programming language and [Fyne](https://fyne.io/) as the GUI framework. To run this programm in a developement enviroment, `cd` into the `cmd` directory and run `go run .` to start the program. 
To run the GUI, make sure you have the necessary dependencies for Fyne installed. See the [Fyne Prerequisites](https://docs.fyne.io/started/quick/#prerequisites).

```
# Clone this repo
git clone https://github.com/Terru03/google-photos-takeout-helper.git
cd google-photos-takeout-helper

# run the hybrid desktop app / CLI entrypoint
go run ./cmd

# build a standalone CLI binary
go build ./cmd/gtf-cli

# run tests
go test ./internal/fixer ./internal/cli

# run lint on the core packages
golangci-lint run ./internal/fixer/... ./internal/cli/...

# build Windows binaries into ./dist
powershell -ExecutionPolicy Bypass -File .\scripts\build-windows.ps1
```

### Docs
- [Architecture](./docs/architecture.md)
- [Troubleshooting](./docs/troubleshooting.md)
- [Versioning](./docs/versioning.md)

## Credits
This project modifies metadata using the [ExifTool](https://exiftool.org/) library by **Phil Harvey**. ExifTool is licensed under the Perl Artistic license, or the GNU General Public License (see [here](https://exiftool.org/#license) for more details).

Matching behavior and product direction were also informed by other Google Takeout repair tools, especially `gophix`, but this codebase maintains its own implementation and state/reporting model.

## Disclaimer
Not affiliated with Google LLC.
