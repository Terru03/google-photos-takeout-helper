package batch

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

const maxSidecarCacheLineBytes = 12 * 1024 * 1024

type sidecarCacheEntry struct {
	Key          string `json:"key"`
	RelativePath string `json:"relativePath"`
	DisplayPath  string `json:"displayPath"`
	Body         []byte `json:"body"`
}

func persistentSidecarCachePath(outputDir string) string {
	return filepath.Join(outputDir, ".gtf", "sidecars", "index.jsonl")
}

func buildPersistentSidecarIndex(
	ctx context.Context,
	cachePath string,
	items []ZipItem,
	onProgress func(completed int, total int, item ZipItem),
) (*fixer.SidecarIndex, int, error) {
	index := fixer.NewSidecarIndex()
	seen := make(map[string]struct{})
	warnings, err := loadSidecarCache(ctx, cachePath, index, seen)
	if err != nil {
		return nil, warnings, err
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return nil, warnings, fmt.Errorf("create sidecar cache folder: %w", err)
	}
	cacheFile, err := os.OpenFile(cachePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, warnings, fmt.Errorf("open sidecar cache: %w", err)
	}
	writer := bufio.NewWriterSize(cacheFile, 256*1024)
	defer func() {
		_ = writer.Flush()
		_ = cacheFile.Close()
	}()

	for itemIndex, item := range items {
		addWarnings := 0
		itemWarnings, visitErr := visitZipSidecars(ctx, item, func(sidecar indexedSidecar) error {
			if _, ok := seen[sidecar.CacheKey]; ok {
				return nil
			}
			entry := sidecarCacheEntry{
				Key:          sidecar.CacheKey,
				RelativePath: sidecar.RelativePath,
				DisplayPath:  sidecar.DisplayPath,
				Body:         sidecar.Body,
			}
			data, marshalErr := json.Marshal(entry)
			if marshalErr != nil {
				return marshalErr
			}
			if _, writeErr := writer.Write(append(data, '\n')); writeErr != nil {
				return writeErr
			}
			seen[sidecar.CacheKey] = struct{}{}
			if addErr := index.AddJSON(sidecar.RelativePath, sidecar.DisplayPath, sidecar.Body); addErr != nil {
				addWarnings++
				fixer.Log(fixer.LoggerWarn, "Parse JSON index entry %s: %v", sidecar.DisplayPath, addErr)
			}
			return nil
		})
		warnings += itemWarnings + addWarnings
		if visitErr != nil {
			return nil, warnings, visitErr
		}
		if err := writer.Flush(); err != nil {
			return nil, warnings, fmt.Errorf("flush sidecar cache: %w", err)
		}
		if err := cacheFile.Sync(); err != nil {
			return nil, warnings, fmt.Errorf("sync sidecar cache: %w", err)
		}
		if onProgress != nil {
			onProgress(itemIndex+1, len(items), item)
		}
	}
	return index, warnings, nil
}

func loadSidecarCache(
	ctx context.Context,
	cachePath string,
	index *fixer.SidecarIndex,
	seen map[string]struct{},
) (int, error) {
	file, err := os.Open(cachePath)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("open sidecar cache: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	warnings := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSidecarCacheLineBytes)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return warnings, err
		}
		var entry sidecarCacheEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			warnings++
			continue
		}
		entry.Key = strings.TrimSpace(entry.Key)
		if entry.Key == "" || len(entry.Body) == 0 {
			warnings++
			continue
		}
		if _, ok := seen[entry.Key]; ok {
			continue
		}
		seen[entry.Key] = struct{}{}
		if err := index.AddJSON(entry.RelativePath, entry.DisplayPath, entry.Body); err != nil {
			warnings++
		}
	}
	if err := scanner.Err(); err != nil {
		return warnings, fmt.Errorf("read sidecar cache: %w", err)
	}
	return warnings, nil
}
