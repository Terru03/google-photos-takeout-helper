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
- **Matching media to sidecars deterministically**, including duplicate suffixes, edited variants, long-name truncation, and live photo partner files.
- **Deduplicating exact duplicate media** across year and album folders by reusing the first output copy instead of storing duplicates again.
- **Resuming safely** with a persisted state file so failed runs can continue instead of restarting from scratch.
- **Producing audit reports** with matched, unmatched, ambiguous, duplicate, conflict, and verification results.
- **Verifying metadata after write** when requested, so the tool can prove what actually landed in the file.

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
> - If your Google Takeout exceeds the 50 GB limit and is split into multiple archives, extract all the archives and move the extracted files into a single folder. This ensures that GoogleTakeoutFixer can process all your files correctly.
> - Select the folder named "Google Photos" as your input folder. This folder should contain subfolders like "Photos from (year)" and folders with the names of your albums. Do not select a parent folder of "Google Photos".

### 2. Installation
1. Download a bundled release of GoogleTakeoutFixer from this repository's releases. Choose the version that matches your operating system.
2. Extract the downloaded archive.
3. Run the executable file.

> [!IMPORTANT]
> When running the executable, a window about security can pop-up if you are using Windows. **Click "more info" and "run anyway"**.

### 3. Using GoogleTakeoutFixer
1. Click **"Select Google Takeout folder"** and choose the folder where you extracted your Google Takeout photos. This folder is named something like "Google Photos".
2. Let the app suggest a sibling output folder automatically, or click **"Select output folder"** and choose your own.
3. Choose the options that you want to apply:
    - **"Write metadata"**: Writes metadata from JSON files into the media files. May not be necessary.
    - **"Use symlinks for albums"**: Creates file links instead of duplicating files for albums.
    - **"Ignore album folders"**: Ignores album folders and only processes year folders.
    - **"Create month subfolders"**: Creates month subfolders (labeled 1-12) inside of the output folders.
    - **"Flatten output structure"**: Puts all files directly in the output folder.
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
- `--month-subfolders`: Create month subfolders (labeled 1-12) inside of folders
- `--flatten`: Flatten the folder structure and put all files directly in the output folder
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

During each run, the tool writes resumable state and audit artifacts under `OUTPUT/.gtf/`:
- `state.jsonl`: append-only processing state used for resumable/idempotent runs
- `reports/latest.txt`: human-readable audit summary
- `reports/latest.json`: detailed machine-readable report
- `logs/*.txt`: per-run logs written beside the repaired library instead of the current working directory

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
