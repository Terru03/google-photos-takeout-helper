Google Photos Takeout Helper was originally bootstrapped from the public `feloex/GoogleTakeoutFixer` repository and further redesigned as the maintained `Terru03/google-photos-takeout-helper` project with additional matching, deduplication, state, reporting, verification, batch ZIP, GUI, and migration-profile work.

It was also informed by ideas explored in the public `gophix` project, particularly around Google Takeout filename quirks and media/JSON pairing edge cases.

If you publish this as your own repository, keep the applicable upstream licenses and attributions intact instead of presenting third-party code as if it originated entirely from scratch.

## Windows x64 bundle

The portable bundle includes ExifTool 13.64 by Phil Harvey, the Windows launcher by Oliver Betz, and the Strawberry Perl runtime from the official ExifTool Windows ZIP. The ExifTool license, Strawberry Perl license archive, and Windows package notes are included under `exiftool_files`. The launcher is named `exiftool.exe` for command-line use.

- [ExifTool 13.64 source archive](https://downloads.sourceforge.net/project/exiftool/Image-ExifTool-13.64.tar.gz)
- [ExifTool Windows package and checksums](https://exiftool.sourceforge.net/index.html)
- [Windows launcher and packaging information](https://oliverbetz.de/pages/Artikel/ExifTool-for-Windows)
- [Strawberry Perl 5.32.1.1 release and source information](https://strawberryperl.com/release-notes/5.32.1.1-64bit.html)
