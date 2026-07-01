package batch

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Terru03/google-photos-takeout-helper/internal/fixer"
)

type workRootStatus struct {
	Path          string
	FreeBytes     int64
	RequiredBytes int64
	Kind          DriveKind
	Usable        bool
	Err           error
}

func resolveWorkDirs(options Options, drives []DriveInfo, zips []ZipItem) ([]string, error) {
	if len(nonEmptyStrings(options.WorkDirs)) > 0 {
		return absPaths(options.WorkDirs)
	}
	if strings.TrimSpace(options.WorkDir) != "" {
		return absPaths([]string{options.WorkDir})
	}

	requiredBytes := maxRequiredWorkBytes(zips, options.SafetyMarginBytes)
	if options.AutoDrives {
		if drive, ok := SelectWorkDrive(drives, requiredBytes, options.OutputDir); ok {
			return []string{filepath.Join(drive.Root, defaultWorkSubdir)}, nil
		}
	}
	if options.AskOnAmbiguous && options.Prompt != nil {
		candidates := drivesWithSpace(drivesExceptOutput(drives, options.OutputDir), requiredBytes)
		drive, err := promptDrive(options, "Choose temporary work drive", candidates, false)
		if err != nil {
			return nil, err
		}
		return []string{filepath.Join(drive.Root, defaultWorkSubdir)}, nil
	}
	return nil, fmt.Errorf("cannot choose temporary work folder with %s free; pass --work or use --ask-on-ambiguous",
		fixer.FormatBytes(requiredBytes),
	)
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func firstWorkDir(workDirs []string) string {
	for _, workDir := range workDirs {
		if strings.TrimSpace(workDir) != "" {
			return workDir
		}
	}
	return ""
}

func ensureWorkRoots(workDirs []string) error {
	for _, workDir := range workDirs {
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			return fmt.Errorf("create work folder %s: %w", workDir, err)
		}
	}
	return nil
}

func chooseWorkRootForZip(item ZipItem, workDirs []string, drives []DriveInfo, marginBytes int64) (workRootStatus, error) {
	required := requiredWorkBytes(item, marginBytes)
	statuses := workRootStatuses(workDirs, drives, required)
	selected, ok := selectBestWorkRoot(statuses)
	if !ok {
		return workRootStatus{}, fmt.Errorf(
			"no work root has enough free space for %s; required %s; %s",
			item.Name,
			fixer.FormatBytes(required),
			formatWorkRootStatuses(statuses),
		)
	}
	fixer.Log(
		fixer.LoggerInfo,
		"Work root selected for %s: %s, free %s, required %s",
		item.Name,
		selected.Path,
		fixer.FormatBytes(selected.FreeBytes),
		fixer.FormatBytes(selected.RequiredBytes),
	)
	return selected, nil
}

func workRootStatuses(workDirs []string, drives []DriveInfo, requiredBytes int64) []workRootStatus {
	statuses := make([]workRootStatus, 0, len(workDirs))
	for _, workDir := range workDirs {
		status := workRootStatus{
			Path:          workDir,
			RequiredBytes: requiredBytes,
			Kind:          driveKindForPath(workDir, drives),
		}
		freeBytes, err := FreeSpace(workDir)
		if err != nil {
			status.Err = err
		} else {
			status.FreeBytes = freeBytes
			status.Usable = requiredBytes <= 0 || freeBytes >= requiredBytes
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func selectBestWorkRoot(statuses []workRootStatus) (workRootStatus, bool) {
	candidates := make([]workRootStatus, 0, len(statuses))
	for _, status := range statuses {
		if status.Err != nil || !status.Usable {
			continue
		}
		candidates = append(candidates, status)
	}
	sort.SliceStable(candidates, func(i int, j int) bool {
		leftRank := driveKindRank(candidates[i].Kind)
		rightRank := driveKindRank(candidates[j].Kind)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		return candidates[i].FreeBytes > candidates[j].FreeBytes
	})
	if len(candidates) == 0 {
		return workRootStatus{}, false
	}
	return candidates[0], true
}

func driveKindRank(kind DriveKind) int {
	switch kind {
	case DriveKindSSD:
		return 2
	case DriveKindUnknown, "":
		return 1
	default:
		return 0
	}
}

func workRootReports(workDirs []string, drives []DriveInfo, requiredBytes int64) []WorkRootReport {
	statuses := workRootStatuses(workDirs, drives, requiredBytes)
	reports := make([]WorkRootReport, 0, len(statuses))
	for _, status := range statuses {
		report := WorkRootReport{
			Path:          status.Path,
			FreeBytes:     status.FreeBytes,
			RequiredBytes: status.RequiredBytes,
			Kind:          status.Kind,
			Usable:        status.Usable,
		}
		if status.Err != nil {
			report.Warning = status.Err.Error()
		} else if !status.Usable {
			report.Warning = fmt.Sprintf("needs %s free, has %s",
				fixer.FormatBytes(status.RequiredBytes),
				fixer.FormatBytes(status.FreeBytes),
			)
		}
		reports = append(reports, report)
	}
	return reports
}

func formatWorkRootStatuses(statuses []workRootStatus) string {
	if len(statuses) == 0 {
		return "no work roots configured"
	}
	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		if status.Err != nil {
			parts = append(parts, fmt.Sprintf("%s free unknown (%v)", status.Path, status.Err))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s free %s required %s",
			status.Path,
			fixer.FormatBytes(status.FreeBytes),
			fixer.FormatBytes(status.RequiredBytes),
		))
	}
	return strings.Join(parts, "; ")
}

func driveKindForPath(path string, drives []DriveInfo) DriveKind {
	root := DriveRoot(path)
	for _, drive := range drives {
		if root != "" && (strings.EqualFold(root, drive.Letter) || strings.EqualFold(root, strings.TrimRight(drive.Root, string(filepath.Separator)))) {
			if drive.Kind != "" {
				return drive.Kind
			}
			return DriveKindUnknown
		}
	}
	return DriveKindUnknown
}

func onlyHDDWorkRoots(reports []WorkRootReport) bool {
	if len(reports) == 0 {
		return false
	}
	for _, report := range reports {
		if report.Kind != DriveKindHDD {
			return false
		}
	}
	return true
}
