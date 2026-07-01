package fixer

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Embed IANA timezone data for self-contained builds.

	"github.com/bradfitz/latlong"
)

type takeoutTimestamp struct {
	Timestamp string `json:"timestamp"`
	Formatted string `json:"formatted"`
}

type takeoutGeoData struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Altitude  float64 `json:"altitude"`
}

type imageMetadata struct {
	Title          string           `json:"title"`
	Description    string           `json:"description"`
	CreationTime   takeoutTimestamp `json:"creationTime"`
	PhotoTakenTime takeoutTimestamp `json:"photoTakenTime"`
	GeoData        takeoutGeoData   `json:"geoData"`
	GeoDataExif    takeoutGeoData   `json:"geoDataExif"`
}

type embeddedMetadata struct {
	CaptureTime time.Time
	Offset      string
	Title       string
	Description string
	GPS         takeoutGeoData
}

type metadataPlan struct {
	CaptureUTC       time.Time
	CaptureLocal     time.Time
	Offset           string
	GPS              takeoutGeoData
	Title            string
	Description      string
	WriteTimestamp   bool
	WriteGPS         bool
	WriteTitle       bool
	WriteDescription bool
	Conflicts        []MetadataFieldConflict
}

type MetadataExpectation struct {
	CaptureUTC  *time.Time
	GPS         *takeoutGeoData
	Title       string
	Description string
}

type MetadataApplyResult struct {
	MetadataWritten bool
	MetadataPlan    MetadataExpectation
	Conflicts       []MetadataFieldConflict
	UsedXMPSidecar  bool
}

func (m imageMetadata) BestTitle() string {
	return strings.TrimSpace(m.Title)
}

func (m imageMetadata) BestDescription() string {
	return strings.TrimSpace(m.Description)
}

func (m imageMetadata) BestGeo() takeoutGeoData {
	if m.GeoData.Latitude != 0 || m.GeoData.Longitude != 0 || m.GeoData.Altitude != 0 {
		return m.GeoData
	}
	return m.GeoDataExif
}

func (m imageMetadata) BestTimestamp() (time.Time, error) {
	for _, candidate := range []string{
		strings.TrimSpace(m.PhotoTakenTime.Timestamp),
		strings.TrimSpace(m.CreationTime.Timestamp),
	} {
		if candidate == "" {
			continue
		}
		timestampInt, err := strconv.ParseInt(candidate, 10, 64)
		if err != nil {
			continue
		}
		return time.Unix(timestampInt, 0).UTC(), nil
	}

	return time.Time{}, fmt.Errorf("takeout JSON has no usable timestamp")
}

func ReadJSONMetadata(jsonPath string) (imageMetadata, error) {
	var data imageMetadata

	jsonFile, err := os.Open(jsonPath)
	if err != nil {
		return data, err
	}
	defer func() {
		_ = jsonFile.Close()
	}()

	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return data, err
	}

	return data, json.Unmarshal(byteValue, &data)
}

func getExifToolPath() string {
	if strings.TrimSpace(exifToolPathOverride) != "" {
		return exifToolPathOverride
	}

	exePath, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exePath)
		exifName := "exiftool"
		if strings.EqualFold(filepath.Ext(exePath), ".exe") {
			exifName = "exiftool.exe"
		}
		bundledPath := filepath.Join(dir, exifName)
		if _, err := os.Stat(bundledPath); err == nil {
			return bundledPath
		}
	}
	return "exiftool"
}

func InitializeExifTool() error {
	_, err := DetectExifTool()
	return err
}

func CloseExifTool() {}

func runExifTool(args ...string) ([]byte, error) {
	cmd, cleanup, err := newExifToolCommand(args...)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func newExifToolCommand(args ...string) (*exec.Cmd, func(), error) {
	if runtime.GOOS != "windows" {
		return newHiddenCommand(getExifToolPath(), args...), func() {}, nil
	}

	argFile, err := os.CreateTemp("", "gtf-exiftool-*.args")
	if err != nil {
		return nil, nil, err
	}

	body := strings.Join(args, "\n") + "\n"
	if _, err := argFile.WriteString(body); err != nil {
		_ = argFile.Close()
		_ = os.Remove(argFile.Name())
		return nil, nil, err
	}
	if err := argFile.Close(); err != nil {
		_ = os.Remove(argFile.Name())
		return nil, nil, err
	}

	cleanup := func() {
		_ = os.Remove(argFile.Name())
	}

	return newHiddenCommand(getExifToolPath(), "-@", argFile.Name()), cleanup, nil
}

func ApplyMetadata(filePath string, meta imageMetadata, policy ConflictPolicy) (MetadataApplyResult, error) {
	plan, err := buildMetadataPlan(meta, policy, filePath)
	if err != nil {
		return MetadataApplyResult{}, err
	}

	result := MetadataApplyResult{
		Conflicts: plan.Conflicts,
	}

	args := []string{"-overwrite_original", "-charset", "filename=utf8"}
	if IsVideoFile(filePath) {
		args = append(args, "-api", "QuickTimeUTC")
	}

	if plan.WriteTimestamp {
		localValue := plan.CaptureLocal.Format("2006:01:02 15:04:05")
		localValueWithTZ := localValue + plan.Offset
		utcValue := plan.CaptureUTC.Format("2006:01:02 15:04:05")

		if IsVideoFile(filePath) {
			args = append(args,
				"-Keys:CreationDate="+localValueWithTZ,
				"-QuickTime:CreateDate="+utcValue,
				"-QuickTime:ModifyDate="+utcValue,
				"-TrackCreateDate="+utcValue,
				"-TrackModifyDate="+utcValue,
				"-MediaCreateDate="+utcValue,
				"-MediaModifyDate="+utcValue,
				"-FileCreateDate="+localValueWithTZ,
				"-FileModifyDate="+localValueWithTZ,
			)
		} else {
			args = append(args,
				"-AllDates="+localValue,
				"-OffsetTime="+plan.Offset,
				"-OffsetTimeOriginal="+plan.Offset,
				"-OffsetTimeDigitized="+plan.Offset,
				"-FileCreateDate="+localValueWithTZ,
				"-FileModifyDate="+localValueWithTZ,
			)
		}
	}

	if plan.WriteGPS {
		if IsVideoFile(filePath) {
			args = append(args,
				"-XMP-exif:GPSLatitude="+fmt.Sprintf("%.7f", plan.GPS.Latitude),
				"-XMP-exif:GPSLongitude="+fmt.Sprintf("%.7f", plan.GPS.Longitude),
				"-XMP-exif:GPSAltitude="+fmt.Sprintf("%.2f", plan.GPS.Altitude),
			)
		} else {
			latRef, lonRef := "N", "E"
			if plan.GPS.Latitude < 0 {
				latRef = "S"
			}
			if plan.GPS.Longitude < 0 {
				lonRef = "W"
			}
			args = append(args,
				"-GPSLatitude="+fmt.Sprintf("%.7f", math.Abs(plan.GPS.Latitude)),
				"-GPSLatitudeRef="+latRef,
				"-GPSLongitude="+fmt.Sprintf("%.7f", math.Abs(plan.GPS.Longitude)),
				"-GPSLongitudeRef="+lonRef,
				"-GPSAltitude="+fmt.Sprintf("%.2f", plan.GPS.Altitude),
				"-XMP-exif:GPSLatitude="+fmt.Sprintf("%.7f", plan.GPS.Latitude),
				"-XMP-exif:GPSLongitude="+fmt.Sprintf("%.7f", plan.GPS.Longitude),
				"-XMP-exif:GPSAltitude="+fmt.Sprintf("%.2f", plan.GPS.Altitude),
			)
		}
	}

	if plan.WriteTitle {
		args = append(args,
			"-Title="+plan.Title,
			"-XMP-dc:Title="+plan.Title,
		)
		if IsVideoFile(filePath) {
			args = append(args, "-Keys:Title="+plan.Title)
		}
	}

	if plan.WriteDescription {
		args = append(args,
			"-Description="+plan.Description,
			"-ImageDescription="+plan.Description,
			"-Caption-Abstract="+plan.Description,
			"-XMP-dc:Description="+plan.Description,
		)
	}

	if !plan.WriteTimestamp && !plan.WriteGPS && !plan.WriteTitle && !plan.WriteDescription {
		return result, nil
	}

	args = append(args, filePath)
	if _, err := runExifTool(args...); err != nil {
		return result, err
	}

	if plan.WriteTimestamp {
		if err := TouchWithCaptureTime(filePath, plan.CaptureUTC); err != nil {
			return result, err
		}
	}

	result.MetadataWritten = true
	result.MetadataPlan = plan.expectation()
	return result, nil
}

func WriteMetadataXMPSidecar(filePath string, meta imageMetadata, policy ConflictPolicy) (MetadataApplyResult, error) {
	plan, err := buildMetadataPlan(meta, policy, filePath)
	if err != nil {
		return MetadataApplyResult{}, err
	}

	result := MetadataApplyResult{
		Conflicts:      plan.Conflicts,
		UsedXMPSidecar: true,
	}
	if !plan.WriteTimestamp && !plan.WriteGPS && !plan.WriteTitle && !plan.WriteDescription {
		return result, nil
	}

	sidecarPath := filePath + ".xmp"
	if err := os.WriteFile(sidecarPath, []byte(renderXMPPacket(plan)), 0o644); err != nil {
		return result, err
	}

	result.MetadataWritten = true
	result.MetadataPlan = plan.expectation()
	return result, nil
}

func renderXMPPacket(plan metadataPlan) string {
	var b strings.Builder
	b.WriteString(`<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>` + "\n")
	b.WriteString(`<x:xmpmeta xmlns:x="adobe:ns:meta/">` + "\n")
	b.WriteString(`  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` + "\n")
	b.WriteString(`    <rdf:Description rdf:about="" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:exif="http://ns.adobe.com/exif/1.0/">` + "\n")
	if plan.WriteTimestamp {
		capture := plan.CaptureLocal.Format("2006-01-02T15:04:05-07:00")
		b.WriteString("      <xmp:CreateDate>" + escapeXML(capture) + "</xmp:CreateDate>\n")
		b.WriteString("      <xmp:ModifyDate>" + escapeXML(capture) + "</xmp:ModifyDate>\n")
	}
	if plan.WriteTitle {
		b.WriteString("      <dc:title><rdf:Alt><rdf:li xml:lang=\"x-default\">" + escapeXML(plan.Title) + "</rdf:li></rdf:Alt></dc:title>\n")
	}
	if plan.WriteDescription {
		b.WriteString("      <dc:description><rdf:Alt><rdf:li xml:lang=\"x-default\">" + escapeXML(plan.Description) + "</rdf:li></rdf:Alt></dc:description>\n")
	}
	if plan.WriteGPS {
		b.WriteString("      <exif:GPSLatitude>" + fmt.Sprintf("%.7f", plan.GPS.Latitude) + "</exif:GPSLatitude>\n")
		b.WriteString("      <exif:GPSLongitude>" + fmt.Sprintf("%.7f", plan.GPS.Longitude) + "</exif:GPSLongitude>\n")
		b.WriteString("      <exif:GPSAltitude>" + fmt.Sprintf("%.2f", plan.GPS.Altitude) + "</exif:GPSAltitude>\n")
	}
	b.WriteString("    </rdf:Description>\n")
	b.WriteString("  </rdf:RDF>\n")
	b.WriteString("</x:xmpmeta>\n")
	b.WriteString(`<?xpacket end="w"?>` + "\n")
	return b.String()
}

func escapeXML(value string) string {
	var b bytes.Buffer
	if err := xml.EscapeText(&b, []byte(value)); err != nil {
		return value
	}
	return b.String()
}

func buildMetadataPlan(meta imageMetadata, policy ConflictPolicy, filePath string) (metadataPlan, error) {
	embedded, _ := ReadEmbeddedMetadata(filePath)
	plan := metadataPlan{}

	plan.Title = meta.BestTitle()
	plan.Description = meta.BestDescription()
	plan.GPS = meta.BestGeo()

	captureUTC, err := meta.BestTimestamp()
	if err == nil {
		location := resolveCaptureLocation(plan.GPS, embedded)
		plan.CaptureUTC = captureUTC
		plan.CaptureLocal = captureUTC.In(location)
		_, offsetSec := plan.CaptureLocal.Zone()
		plan.Offset = formatTimezoneOffset(offsetSec)
	}

	plan.Conflicts = detectConflicts(meta, embedded)

	switch policy {
	case ConflictPreferEmbedded:
		plan.WriteTimestamp = !plan.CaptureUTC.IsZero() && embedded.CaptureTime.IsZero()
		plan.WriteGPS = hasGeo(plan.GPS) && !hasGeo(embedded.GPS)
		plan.WriteTitle = plan.Title != "" && strings.TrimSpace(embedded.Title) == ""
		plan.WriteDescription = plan.Description != "" && strings.TrimSpace(embedded.Description) == ""
	case ConflictMerge:
		plan.WriteTimestamp = !plan.CaptureUTC.IsZero()
		plan.WriteGPS = hasGeo(plan.GPS)
		plan.WriteTitle = plan.Title != "" && strings.TrimSpace(embedded.Title) == ""
		plan.WriteDescription = plan.Description != "" && strings.TrimSpace(embedded.Description) == ""
	default:
		plan.WriteTimestamp = !plan.CaptureUTC.IsZero()
		plan.WriteGPS = hasGeo(plan.GPS)
		plan.WriteTitle = plan.Title != ""
		plan.WriteDescription = plan.Description != ""
	}

	return plan, nil
}

func (p metadataPlan) expectation() MetadataExpectation {
	expectation := MetadataExpectation{
		Title:       p.Title,
		Description: p.Description,
	}
	if p.WriteTimestamp {
		captureUTC := p.CaptureUTC
		expectation.CaptureUTC = &captureUTC
	}
	if p.WriteGPS {
		gps := p.GPS
		expectation.GPS = &gps
	}
	if !p.WriteTitle {
		expectation.Title = ""
	}
	if !p.WriteDescription {
		expectation.Description = ""
	}
	return expectation
}

func ReadEmbeddedMetadata(filePath string) (embeddedMetadata, error) {
	var result embeddedMetadata

	output, err := runExifTool(
		"-j",
		"-n",
		"-api", "QuickTimeUTC",
		"-charset", "filename=utf8",
		"-DateTimeOriginal",
		"-CreateDate",
		"-QuickTime:CreateDate",
		"-TrackCreateDate",
		"-MediaCreateDate",
		"-Keys:CreationDate",
		"-OffsetTime",
		"-OffsetTimeOriginal",
		"-Title",
		"-Description",
		"-ImageDescription",
		"-Caption-Abstract",
		"-GPSLatitude",
		"-GPSLongitude",
		"-GPSAltitude",
		filePath,
	)
	if err != nil {
		return result, err
	}

	var payload []map[string]interface{}
	if err := json.Unmarshal(output, &payload); err != nil {
		return result, err
	}
	if len(payload) == 0 {
		return result, nil
	}

	row := payload[0]
	result.Offset = firstNonEmptyString(
		toString(row["OffsetTimeOriginal"]),
		toString(row["OffsetTime"]),
	)
	result.Title = firstNonEmptyString(toString(row["Title"]))
	result.Description = firstNonEmptyString(
		toString(row["Description"]),
		toString(row["ImageDescription"]),
		toString(row["Caption-Abstract"]),
	)
	result.GPS = takeoutGeoData{
		Latitude:  toFloat(row["GPSLatitude"]),
		Longitude: toFloat(row["GPSLongitude"]),
		Altitude:  toFloat(row["GPSAltitude"]),
	}

	rawTime := firstNonEmptyString(
		toString(row["DateTimeOriginal"]),
		toString(row["CreationDate"]),
		toString(row["Keys:CreationDate"]),
		toString(row["CreateDate"]),
		toString(row["QuickTime:CreateDate"]),
		toString(row["TrackCreateDate"]),
		toString(row["MediaCreateDate"]),
	)
	if rawTime != "" {
		if parsed, err := parseFlexibleExifTime(rawTime, result.Offset); err == nil {
			result.CaptureTime = parsed
		}
	}

	return result, nil
}

func VerifyMetadata(filePath string, expected MetadataExpectation) error {
	actual, err := ReadEmbeddedMetadata(filePath)
	if err != nil {
		return err
	}

	var problems []string
	if expected.CaptureUTC != nil {
		if actual.CaptureTime.IsZero() {
			problems = append(problems, "capture time missing after write")
		} else if math.Abs(actual.CaptureTime.UTC().Sub(expected.CaptureUTC.UTC()).Seconds()) > 2 {
			problems = append(problems, fmt.Sprintf("capture time mismatch (got %s)", actual.CaptureTime.UTC().Format(time.RFC3339)))
		}
	}

	if expected.GPS != nil {
		if !hasGeo(actual.GPS) {
			problems = append(problems, "GPS coordinates missing after write")
		} else {
			if math.Abs(actual.GPS.Latitude-expected.GPS.Latitude) > 0.0001 {
				problems = append(problems, "latitude mismatch")
			}
			if math.Abs(actual.GPS.Longitude-expected.GPS.Longitude) > 0.0001 {
				problems = append(problems, "longitude mismatch")
			}
		}
	}

	if expected.Title != "" && strings.TrimSpace(actual.Title) != expected.Title {
		problems = append(problems, "title mismatch")
	}
	if expected.Description != "" && strings.TrimSpace(actual.Description) != expected.Description {
		problems = append(problems, "description mismatch")
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func GetMajorBrand(filePath string) (string, error) {
	output, err := runExifTool("-s3", "-MajorBrand", "-charset", "filename=utf8", filePath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func detectConflicts(meta imageMetadata, embedded embeddedMetadata) []MetadataFieldConflict {
	conflicts := make([]MetadataFieldConflict, 0, 4)

	if captureUTC, err := meta.BestTimestamp(); err == nil && !embedded.CaptureTime.IsZero() {
		if math.Abs(embedded.CaptureTime.UTC().Sub(captureUTC.UTC()).Seconds()) > 2 {
			conflicts = append(conflicts, MetadataFieldConflict{
				Field:         "capture-time",
				JSONValue:     captureUTC.Format(time.RFC3339),
				EmbeddedValue: embedded.CaptureTime.UTC().Format(time.RFC3339),
			})
		}
	}

	jsonGeo := meta.BestGeo()
	if hasGeo(jsonGeo) && hasGeo(embedded.GPS) {
		if math.Abs(jsonGeo.Latitude-embedded.GPS.Latitude) > 0.0001 ||
			math.Abs(jsonGeo.Longitude-embedded.GPS.Longitude) > 0.0001 {
			conflicts = append(conflicts, MetadataFieldConflict{
				Field:         "gps",
				JSONValue:     fmt.Sprintf("%.6f,%.6f", jsonGeo.Latitude, jsonGeo.Longitude),
				EmbeddedValue: fmt.Sprintf("%.6f,%.6f", embedded.GPS.Latitude, embedded.GPS.Longitude),
			})
		}
	}

	if title := meta.BestTitle(); title != "" && strings.TrimSpace(embedded.Title) != "" && title != strings.TrimSpace(embedded.Title) {
		conflicts = append(conflicts, MetadataFieldConflict{
			Field:         "title",
			JSONValue:     title,
			EmbeddedValue: strings.TrimSpace(embedded.Title),
		})
	}

	if description := meta.BestDescription(); description != "" && strings.TrimSpace(embedded.Description) != "" && description != strings.TrimSpace(embedded.Description) {
		conflicts = append(conflicts, MetadataFieldConflict{
			Field:         "description",
			JSONValue:     description,
			EmbeddedValue: strings.TrimSpace(embedded.Description),
		})
	}

	return conflicts
}

func resolveCaptureLocation(geo takeoutGeoData, embedded embeddedMetadata) *time.Location {
	if hasGeo(geo) {
		return getPhotoTimezone(geo.Latitude, geo.Longitude)
	}
	if embedded.Offset != "" {
		return fixedZoneFromOffset(embedded.Offset)
	}
	if !embedded.CaptureTime.IsZero() {
		return embedded.CaptureTime.Location()
	}
	return time.UTC
}

func getPhotoTimezone(lat float64, lon float64) *time.Location {
	if lat == 0 && lon == 0 {
		return time.UTC
	}

	tzName := latlong.LookupZoneName(lat, lon)
	if tzName == "" {
		Log(LoggerWarn, "Could not look up timezone for coordinates lat=%f, lon=%f", lat, lon)
		offsetSec := int(math.Round(lon/15.0)) * 3600
		return time.FixedZone("Photo", offsetSec)
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		Log(LoggerWarn, "Could not load timezone '%s'", tzName)
		offsetSec := int(math.Round(lon/15.0)) * 3600
		return time.FixedZone("Photo", offsetSec)
	}
	return loc
}

func fixedZoneFromOffset(offset string) *time.Location {
	if len(offset) != 6 {
		return time.UTC
	}
	sign := 1
	if strings.HasPrefix(offset, "-") {
		sign = -1
	}
	hours, _ := strconv.Atoi(offset[1:3])
	minutes, _ := strconv.Atoi(offset[4:6])
	totalSeconds := sign * ((hours * 3600) + (minutes * 60))
	return time.FixedZone("Offset", totalSeconds)
}

func formatTimezoneOffset(offsetSec int) string {
	sign := "+"
	if offsetSec < 0 {
		sign = "-"
		offsetSec = -offsetSec
	}
	hours := offsetSec / 3600
	minutes := (offsetSec % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, hours, minutes)
}

func parseFlexibleExifTime(raw string, fallbackOffset string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}

	formats := []string{
		"2006:01:02 15:04:05-07:00",
		"2006:01:02 15:04:05Z07:00",
		time.RFC3339,
	}
	for _, format := range formats {
		if parsed, err := time.Parse(format, raw); err == nil {
			return parsed, nil
		}
	}

	if fallbackOffset != "" {
		if parsed, err := time.Parse("2006:01:02 15:04:05-07:00", raw+fallbackOffset); err == nil {
			return parsed, nil
		}
	}

	return time.ParseInLocation("2006:01:02 15:04:05", raw, time.UTC)
}

func hasGeo(geo takeoutGeoData) bool {
	return geo.Latitude != 0 || geo.Longitude != 0
}

func toString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func toFloat(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case string:
		number, _ := strconv.ParseFloat(typed, 64)
		return number
	default:
		return 0
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
