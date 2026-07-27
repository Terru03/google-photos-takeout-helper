package batch

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

const maxIndexedSidecarBytes = uint64(8 * 1024 * 1024)

func buildGlobalSidecarIndex(
	ctx context.Context,
	items []ZipItem,
	onProgress func(completed int, total int, item ZipItem),
) (*fixer.SidecarIndex, int, error) {
	index := fixer.NewSidecarIndex()
	warnings := 0

	for itemIndex, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, warnings, err
		}
		fixer.Log(fixer.LoggerInfo, "Index ZIP %d/%d: %s", itemIndex+1, len(items), item.Path)
		reader, err := zip.OpenReader(item.Path)
		if err != nil {
			return nil, warnings, fmt.Errorf("open ZIP for JSON index %s: %w", item.Path, err)
		}

		for _, file := range reader.File {
			if err := ctx.Err(); err != nil {
				_ = reader.Close()
				return nil, warnings, err
			}
			if file.FileInfo().IsDir() || !isIndexableSidecarName(file.Name) {
				continue
			}

			safeName, safeErr := safeZipEntryName(file.Name)
			if safeErr != nil {
				warnings++
				fixer.Log(fixer.LoggerWarn, "Skip unsafe JSON index entry %s in %s: %v", file.Name, item.Path, safeErr)
				continue
			}
			relativePath, ok := googlePhotosRelativePath(safeName)
			if !ok {
				continue
			}
			if file.UncompressedSize64 > maxIndexedSidecarBytes {
				warnings++
				fixer.Log(fixer.LoggerWarn, "Skip large JSON index entry %s in %s", file.Name, item.Path)
				continue
			}

			source, openErr := file.Open()
			if openErr != nil {
				warnings++
				fixer.Log(fixer.LoggerWarn, "Read JSON index entry %s in %s: %v", file.Name, item.Path, openErr)
				continue
			}
			body, readErr := io.ReadAll(io.LimitReader(source, int64(maxIndexedSidecarBytes)+1))
			closeErr := source.Close()
			if readErr != nil || closeErr != nil || uint64(len(body)) > maxIndexedSidecarBytes {
				warnings++
				fixer.Log(fixer.LoggerWarn, "Read JSON index entry %s in %s: %v", file.Name, item.Path, firstError(readErr, closeErr))
				continue
			}

			displayPath := item.Path + "::" + filepath.ToSlash(safeName)
			if addErr := index.AddJSON(relativePath, displayPath, body); addErr != nil {
				warnings++
				fixer.Log(fixer.LoggerWarn, "Parse JSON index entry %s in %s: %v", file.Name, item.Path, addErr)
			}
		}
		if closeErr := reader.Close(); closeErr != nil {
			return nil, warnings, fmt.Errorf("close ZIP after JSON index %s: %w", item.Path, closeErr)
		}
		if onProgress != nil {
			onProgress(itemIndex+1, len(items), item)
		}
	}

	return index, warnings, nil
}

func isIndexableSidecarName(name string) bool {
	if !strings.EqualFold(filepath.Ext(name), ".json") {
		return false
	}
	return !strings.EqualFold(filepath.Base(name), "user-generated-memory-titles.json")
}

func googlePhotosRelativePath(safeName string) (string, bool) {
	parts := strings.Split(filepath.Clean(safeName), string(filepath.Separator))
	for index, part := range parts {
		if !strings.EqualFold(part, "Google Photos") {
			continue
		}
		if index+1 >= len(parts) {
			return "", false
		}
		return filepath.Join(parts[index+1:]...), true
	}
	return "", false
}

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return fmt.Errorf("JSON entry exceeds size limit")
}
