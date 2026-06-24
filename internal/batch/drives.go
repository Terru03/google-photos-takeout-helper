package batch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/feloex/GoogleTakeoutFixer/internal/fixer"
)

type psDrive struct {
	Letter     string `json:"Letter"`
	Label      string `json:"Label"`
	TotalBytes int64  `json:"TotalBytes"`
	FreeBytes  int64  `json:"FreeBytes"`
	DriveType  string `json:"DriveType"`
	Model      string `json:"Model"`
	MediaType  string `json:"MediaType"`
}

func DetectDrives() ([]DriveInfo, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("drive scan is only available on Windows")
	}

	command := `
$ErrorActionPreference = "SilentlyContinue"
$vols = Get-Volume | Where-Object { $_.DriveLetter -and $_.Size -gt 0 }
$rows = foreach ($v in $vols) {
  $disk = $null
  try {
    $part = Get-Partition -DriveLetter $v.DriveLetter | Select-Object -First 1
    if ($part) { $disk = Get-Disk -Number $part.DiskNumber }
  } catch {}
  [PSCustomObject]@{
    Letter = ($v.DriveLetter + ":")
    Label = $v.FileSystemLabel
    TotalBytes = [int64]$v.Size
    FreeBytes = [int64]$v.SizeRemaining
    DriveType = [string]$v.DriveType
    Model = if ($disk) { [string]$disk.FriendlyName } else { "" }
    MediaType = if ($disk) { [string]$disk.MediaType } else { "" }
  }
}
$rows | Sort-Object Letter | ConvertTo-Json -Compress
`
	output, err := exec.Command("powershell.exe", "-NoProfile", "-Command", command).Output()
	if err != nil {
		return nil, fmt.Errorf("scan Windows drives: %w", err)
	}

	output = bytes.TrimSpace(output)
	if len(output) == 0 || bytes.Equal(output, []byte("null")) {
		return nil, nil
	}

	var rows []psDrive
	if output[0] == '[' {
		if err := json.Unmarshal(output, &rows); err != nil {
			return nil, fmt.Errorf("parse drive scan: %w", err)
		}
	} else {
		var row psDrive
		if err := json.Unmarshal(output, &row); err != nil {
			return nil, fmt.Errorf("parse drive scan: %w", err)
		}
		rows = append(rows, row)
	}

	drives := make([]DriveInfo, 0, len(rows))
	for _, row := range rows {
		letter := strings.ToUpper(strings.TrimSpace(row.Letter))
		if letter == "" {
			continue
		}
		root := letter + string(filepath.Separator)
		drives = append(drives, DriveInfo{
			Root:       root,
			Letter:     letter,
			Label:      strings.TrimSpace(row.Label),
			TotalBytes: row.TotalBytes,
			FreeBytes:  row.FreeBytes,
			Kind:       detectDriveKind(row.MediaType, row.Model),
			Model:      strings.TrimSpace(row.Model),
		})
	}

	return drives, nil
}

func detectDriveKind(mediaType string, model string) DriveKind {
	value := strings.ToLower(mediaType + " " + model)
	switch {
	case strings.Contains(value, "ssd") || strings.Contains(value, "solid state"):
		return DriveKindSSD
	case strings.Contains(value, "hdd") || strings.Contains(value, "hard disk"):
		return DriveKindHDD
	default:
		return DriveKindUnknown
	}
}

func SelectOutputDrive(drives []DriveInfo) (DriveInfo, bool, error) {
	var labelMatches []DriveInfo
	var letterMatches []DriveInfo
	for _, drive := range drives {
		if strings.EqualFold(strings.TrimSpace(drive.Label), "Backup B") {
			labelMatches = append(labelMatches, drive)
		}
		if strings.EqualFold(strings.TrimSpace(drive.Letter), "B:") {
			letterMatches = append(letterMatches, drive)
		}
	}

	if len(labelMatches) > 1 {
		return DriveInfo{}, false, fmt.Errorf("more than one drive has label Backup B")
	}
	if len(labelMatches) == 1 {
		return labelMatches[0], true, nil
	}
	if len(letterMatches) > 1 {
		return DriveInfo{}, false, fmt.Errorf("more than one B: drive found")
	}
	if len(letterMatches) == 1 {
		return letterMatches[0], true, nil
	}
	return DriveInfo{}, false, nil
}

func SelectWorkDrive(drives []DriveInfo, requiredBytes int64, outputDir string) (DriveInfo, bool) {
	outputRoot := strings.ToUpper(filepath.VolumeName(outputDir))
	candidates := make([]DriveInfo, 0, len(drives))
	for _, drive := range drives {
		if drive.Kind != DriveKindSSD {
			continue
		}
		if requiredBytes > 0 && drive.FreeBytes < requiredBytes {
			continue
		}
		if outputRoot != "" && strings.EqualFold(drive.Letter, outputRoot) {
			continue
		}
		candidates = append(candidates, drive)
	}

	sort.SliceStable(candidates, func(i int, j int) bool {
		return candidates[i].FreeBytes > candidates[j].FreeBytes
	})
	if len(candidates) == 0 {
		return DriveInfo{}, false
	}
	return candidates[0], true
}

func DriveRoot(path string) string {
	volume := filepath.VolumeName(path)
	if volume != "" {
		return strings.ToUpper(volume)
	}
	if filepath.IsAbs(path) {
		return string(filepath.Separator)
	}
	return ""
}

func FormatDrive(drive DriveInfo) string {
	kind := string(drive.Kind)
	if kind == "" {
		kind = string(DriveKindUnknown)
	}
	label := drive.Label
	if label == "" {
		label = "(no label)"
	}
	return fmt.Sprintf(
		"%s label=%s total=%s free=%s type=%s",
		drive.Letter,
		label,
		fixer.FormatBytes(drive.TotalBytes),
		fixer.FormatBytes(drive.FreeBytes),
		kind,
	)
}
