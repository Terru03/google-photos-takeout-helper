package fixer

import (
	"math"
	"strings"
	"time"
)

func detectSuspiciousDates(plan MediaPlan, outputPath string) []SuspiciousDateFinding {
	jsonTimestamp := ""
	embeddedTimestamp := ""
	reasons := make([]string, 0, 4)

	if plan.Metadata == nil {
		reasons = append(reasons, "missing JSON timestamp")
		return suspiciousDateRows(plan.SourcePath, outputPath, jsonTimestamp, embeddedTimestamp, reasons)
	}

	jsonTime, err := plan.Metadata.BestTimestamp()
	if err != nil {
		reasons = append(reasons, "missing JSON timestamp")
		return suspiciousDateRows(plan.SourcePath, outputPath, jsonTimestamp, embeddedTimestamp, reasons)
	}
	jsonTimestamp = jsonTime.UTC().Format(time.RFC3339)

	if jsonTime.UTC().Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
		reasons = append(reasons, "timestamp before 2000")
	}
	if jsonTime.UTC().After(time.Now().UTC()) {
		reasons = append(reasons, "timestamp in the future")
	}

	embedded, embeddedErr := ReadEmbeddedMetadata(plan.SourcePath)
	if embeddedErr == nil {
		if !embedded.CaptureTime.IsZero() {
			embeddedTimestamp = embedded.CaptureTime.UTC().Format(time.RFC3339)
			if math.Abs(embedded.CaptureTime.UTC().Sub(jsonTime.UTC()).Hours()) > 24 {
				reasons = append(reasons, "JSON timestamp differs from embedded timestamp by more than 24h")
			}
		}
		if !hasGeo(plan.Metadata.BestGeo()) && strings.TrimSpace(embedded.Offset) == "" {
			reasons = append(reasons, "timezone guessed as UTC because no GPS/offset existed")
		}
	}

	return suspiciousDateRows(plan.SourcePath, outputPath, jsonTimestamp, embeddedTimestamp, reasons)
}

func suspiciousDateRows(sourcePath string, outputPath string, jsonTimestamp string, embeddedTimestamp string, reasons []string) []SuspiciousDateFinding {
	if len(reasons) == 0 {
		return nil
	}
	return []SuspiciousDateFinding{{
		SourcePath:        sourcePath,
		OutputPath:        outputPath,
		JSONTimestamp:     jsonTimestamp,
		EmbeddedTimestamp: embeddedTimestamp,
		Reason:            strings.Join(reasons, "; "),
	}}
}
