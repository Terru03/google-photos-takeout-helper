# Migration workflows

GoogleTakeoutFixer prepares a repaired local library from Google Photos Takeout. It does not directly upload photos to cloud services or photo servers.

The normal pattern is:

1. Export from Google Takeout.
2. Repair locally with GoogleTakeoutFixer.
3. Review reports under `OUTPUT/.gtf/`.
4. Import the repaired output into your chosen library, NAS, or photo-management tool.

## Archive-safe workflow

Best when the priority is long-term preservation.

Recommended output set:

```text
Original Takeout ZIPs
Repaired local library
OUTPUT/.gtf/ reports, logs, and state
```

Recommended settings:

- run Audit Only first;
- run Recommended Safe Mode;
- enable verification if time allows;
- keep suspicious date reports;
- avoid source deletion;
- keep original ZIPs on a second drive if possible.

Why this matters:

- original exports remain available if a tool has a bug;
- repaired output is easier to browse and import;
- `.gtf` reports show what happened during processing.

## Immich-ready local library

Best when you plan to import the repaired library into Immich using Immich's own tools or community import tools.

GoogleTakeoutFixer should be used before upload/import to:

- restore dates and GPS metadata where possible;
- reduce exact duplicate output;
- produce a clear folder library;
- report ambiguous and unmatched files;
- keep a migration proof under `.gtf/`.

Suggested workflow:

1. Keep original Takeout ZIPs.
2. Run preflight or Audit Only.
3. Run a safe repair into a local output folder.
4. Review `OUTPUT/.gtf/reports/latest.txt` and `suspicious_dates.csv`.
5. Import the repaired output using your chosen Immich workflow.

Do not claim the archive is clean until unmatched, ambiguous, suspicious, and failed verification records have been reviewed.

Future improvement:

- a dedicated Immich-ready output profile;
- optional album manifest;
- generated suggested import command;
- optional XMP sidecars if supported later.

## Synology, NAS, or normal folder library

Best when you want a clean folder tree on an external drive or NAS.

Useful options:

- create month subfolders for easier browsing;
- keep year and album structure if you still want albums visible;
- use symlinks for albums only when the target filesystem and tools handle them well;
- flatten output only if folder structure does not matter to you.

Suggested workflow:

```sh
./GoogleTakeoutFixer --input "/path/to/takeout/Google Photos/" --output "/path/to/Google Photos Fixed/" --verify
```

After processing:

- copy or sync the repaired output to the NAS;
- keep `.gtf` reports beside the repaired library;
- keep the original Takeout ZIPs separately.

## Lightroom or digiKam workflow

Current workflow:

- repair metadata into media files;
- import the repaired folder library into Lightroom, digiKam, or another photo manager;
- keep the `.gtf` reports for audit.

Future XMP workflow:

- write metadata into XMP sidecars instead of, or in addition to, media files;
- keep media files unchanged when possible;
- import using software that reads XMP sidecars.

Do not present XMP sidecar mode as finished until it is implemented and tested.

## Motion Photo workflow

Best when your Takeout includes live/motion photo pairs and you want Windows-viewable Motion Photos.

Requirements:

- MotionPhoto2 installed separately or placed beside the app binary;
- `--motion-photos` enabled;
- enough free space to keep partner videos if using `--keep-live-video`.

Recommended safe command:

```powershell
.\gtf-cli.exe --input "D:\Takeout\Google Photos" --output "B:\Google_Photos_Final" --motion-photos --keep-live-video --verify
```

Review the report for:

- motion pairs detected;
- embeds completed;
- videos kept;
- videos deleted, if cleanup was enabled;
- failures.

## Choosing a workflow

| Goal | Suggested workflow |
|---|---|
| Preserve everything with maximum safety | Archive-safe workflow |
| Import into Immich later | Immich-ready local library |
| Browse from external drive or NAS | Synology / normal folder library |
| Use desktop photo-management tools | Lightroom / digiKam workflow |
| Preserve Windows-viewable Motion Photos | Motion Photo workflow |

## What not to do

Avoid these workflows:

- deleting original Takeout ZIPs before reviewing the repaired output;
- writing output inside the input folder;
- writing output inside the temp work folder;
- treating a clean-looking folder tree as proof without checking reports;
- using destructive cleanup on a first run;
- importing into another platform before reviewing unmatched, ambiguous, suspicious, and failed verification records.
