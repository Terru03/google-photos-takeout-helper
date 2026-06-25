/*
GoogleTakeoutFixer - A tool to easily clean and organize Google Photos Takeout exports
Copyright (C) 2026 feloex

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/feloex/GoogleTakeoutFixer/internal/batch"
	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
	version "github.com/feloex/GoogleTakeoutFixer/internal/version"
)

type stringListFlag []string

func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func Main() {
	defer fixer.RecoverPanic("cli-main")

	// Handle logs from the fixer package by printing them
	fixer.SetLogHandler(func(level fixer.LogLevel, message string) {
		fmt.Printf("[%s] %s\n", level, message)
	})

	defaults := fixer.DefaultProcessOptions()

	// Command-line flags
	showVersion := flag.Bool("version", false, "Show current version")
	inputPath := flag.String("input", "", "Path to Google takeout directory")
	outputPath := flag.String("output", "", "Path to output directory")
	batchZips := flag.Bool("batch-zips", false, "Process huge Google Takeout ZIP exports one ZIP at a time")
	var zipRoots stringListFlag
	flag.Var(&zipRoots, "zip-root", "Path to a Takeout ZIP file or folder containing Takeout ZIP files; may be repeated")
	workPath := flag.String("work", "", "Temporary work folder used for extracting one ZIP at a time")
	autoDrives := flag.Bool("auto-drives", false, "Scan Windows drives and choose safe defaults where clear")
	askOnAmbiguous := flag.Bool("ask-on-ambiguous", false, "Ask before choosing ambiguous drives or continuing after a problem report")
	keepTempOnError := flag.Bool("keep-temp-on-error", false, "Keep temporary extracted files when a ZIP fails or needs review")
	keepLiveVideo := flag.Bool("keep-live-video", false, "Keep standalone live-video files after MotionPhoto2 embeds them")
	preflightOnly := flag.Bool("preflight-only", false, "Scan batch ZIPs and print space/path warnings without extracting or processing")
	reprocessBatch := flag.Bool("reprocess", false, "Reprocess ZIPs even if the batch manifest already marks them successful")
	useSymlinks := flag.Bool("symlink", false, "Use symlinks inside of albums instead of duplicating images")
	skipMetadata := flag.Bool("skip-metadata", !defaults.WriteMetadata, "Skip writing metadata to files")
	ignoreAlbums := flag.Bool("ignore-albums", false, "Ignore all album folders")
	monthSubfolders := flag.Bool("month-subfolders", false, "Create month subfolders like \"1 - January\" inside year folders")
	flatten := flag.Bool("flatten", false, "Put all media files directly in the output folder without year/album subfolders")
	createMotionPhotos := flag.Bool("motion-photos", false, "Create Windows-viewable Samsung/Google Motion Photos with MotionPhoto2 after processing")
	deleteSource := flag.Bool("delete-source", false, "Delete the original input folder after a fully clean run with zero unmatched, ambiguous, or error records")
	restoreMOV := flag.Bool("restore-mov", defaults.RestoreMOVExtension, "Restore .MOV file extension in case the Major Brand EXIF field says \"Apple QuickTime (.MOV/QT)\"")
	dryRun := flag.Bool("dry-run", false, "Plan the run and generate an audit report without writing files")
	verifyWrites := flag.Bool("verify", defaults.VerifyWrites, "Verify written metadata by reading it back with ExifTool")
	noDeduplicate := flag.Bool("no-deduplicate", !defaults.Deduplicate, "Keep duplicate files instead of linking or reusing exact matches")
	conflictPolicyValue := flag.String("conflict-policy", string(defaults.ConflictPolicy), "How to handle conflicts between embedded metadata and Takeout JSON: prefer-json, prefer-embedded, merge")

	flag.Parse()
	keepLiveVideoProvided := boolFlagProvided("keep-live-video")

	if *showVersion {
		fmt.Println(version.Tag)
		return
	}

	if *flatten && *useSymlinks {
		fmt.Println("Error: --flatten and --symlink cannot be used together")
		os.Exit(1)
	}
	if *flatten && *ignoreAlbums {
		fmt.Println("Error: --flatten and --ignore-albums cannot be used together")
		os.Exit(1)
	}
	if *flatten && *monthSubfolders {
		fmt.Println("Error: --flatten and --month-subfolders cannot be used together")
		os.Exit(1)
	}
	if *useSymlinks && *ignoreAlbums {
		fmt.Println("Error: --symlink and --ignore-albums cannot be used together")
		os.Exit(1)
	}

	conflictPolicy, err := fixer.ParseConflictPolicy(*conflictPolicyValue)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	options := fixer.ProcessOptions{
		UseSymlinks:              *useSymlinks,
		WriteMetadata:            !*skipMetadata,
		Flatten:                  *flatten,
		IgnoreAlbums:             *ignoreAlbums,
		MonthSubfolders:          *monthSubfolders,
		CreateMotionPhotos:       *createMotionPhotos,
		KeepLiveVideo:            *keepLiveVideo,
		DeleteSourceAfterSuccess: *deleteSource,
		RestoreMOVExtension:      *restoreMOV,
		Deduplicate:              !*noDeduplicate,
		DryRun:                   *dryRun,
		VerifyWrites:             *verifyWrites,
		ConflictPolicy:           conflictPolicy,
	}

	if *batchZips {
		if !keepLiveVideoProvided {
			options.KeepLiveVideo = true
		}
		runBatchZIPMode(batch.Options{
			ZipRoots:        []string(zipRoots),
			WorkDir:         *workPath,
			OutputDir:       *outputPath,
			AutoDrives:      *autoDrives,
			AskOnAmbiguous:  *askOnAmbiguous || *autoDrives,
			KeepTempOnError: *keepTempOnError,
			PreflightOnly:   *preflightOnly,
			Reprocess:       *reprocessBatch,
			ProcessOptions:  options,
			Prompt:          cliPrompt,
		})
		return
	}

	if *inputPath == "" || *outputPath == "" {
		fmt.Println("Error: --input and --output are required")
		flag.Usage()
		os.Exit(1)
	}

	progressCh := make(chan fixer.Progress)

	if err := fixer.ValidateProcessPaths(*inputPath, *outputPath); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	if exifInfo, err := fixer.ValidateProcessingDependencies(options); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	} else if exifInfo != nil {
		fmt.Printf("Using ExifTool %s from %s\n", exifInfo.Version, exifInfo.Path)
	}
	if motionInfo, err := fixer.ValidateMotionPhotoDependencies(options); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	} else if motionInfo != nil {
		fmt.Printf("Using MotionPhoto2 from %s\n", motionInfo.Path)
	}

	fixer.SafeGo("cli-process", func() {
		// Invert skipMetadata because the flag is named skipMetadata but the process function expects writeMetadata
		if err := fixer.Process(context.Background(), *inputPath, *outputPath, progressCh, options); err != nil {
			fmt.Printf("Error during processing: %v\n", err)
		}
	})

	for p := range progressCh {
		if p.Processed == 0 {
			continue
		}

		percentageFinished := math.Round(float64(p.Processed) / float64(p.Total) * 100)

		fmt.Printf("[%3.0f%%] %2d/%2d - %s\n",
			percentageFinished,
			p.Processed,
			p.Total,
			filepath.Base(p.Current),
		)
	}

	fmt.Println("\nDone")
}

func runBatchZIPMode(options batch.Options) {
	if options.ProcessOptions.DeleteSourceAfterSuccess {
		fmt.Println("Batch ZIP mode ignores --delete-source because ZIP sources must never be deleted")
		options.ProcessOptions.DeleteSourceAfterSuccess = false
	}

	if !options.ProcessOptions.DryRun {
		if exifInfo, err := fixer.ValidateProcessingDependencies(options.ProcessOptions); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		} else if exifInfo != nil {
			fmt.Printf("Using ExifTool %s from %s\n", exifInfo.Version, exifInfo.Path)
		}
		if motionInfo, err := fixer.ValidateMotionPhotoDependencies(options.ProcessOptions); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		} else if motionInfo != nil {
			fmt.Printf("Using MotionPhoto2 from %s\n", motionInfo.Path)
		}
	}

	result, err := batch.Run(context.Background(), options)
	if err != nil {
		fmt.Printf("Error during batch ZIP processing: %v\n", err)
		os.Exit(1)
	}
	if result.Preflight != nil {
		fmt.Println(batch.FormatPreflightReport(*result.Preflight))
		return
	}

	fmt.Printf("\nBatch done\n")
	fmt.Printf("ZIPs found: %d\n", result.ZipCount)
	fmt.Printf("Processed: %d\n", result.Processed)
	fmt.Printf("Skipped: %d\n", result.Skipped)
	fmt.Printf("Planned: %d\n", result.Planned)
	fmt.Printf("Output: %s\n", result.OutputDir)
	if result.WorkDir != "" {
		fmt.Printf("Work: %s\n", result.WorkDir)
	}
	fmt.Printf("Manifest: %s\n", result.ManifestPath)
}

func boolFlagProvided(name string) bool {
	provided := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}

func cliPrompt(question string, choices []string, allowMultiple bool) (string, error) {
	fmt.Println(question)
	for _, choice := range choices {
		fmt.Println(choice)
	}
	if allowMultiple {
		fmt.Print("Enter number, drive letter, or comma list: ")
	} else {
		fmt.Print("Enter number or drive letter: ")
	}
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(answer), nil
}
