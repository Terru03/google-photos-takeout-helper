package fixer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func withFakeExifTool(t *testing.T, extraEnv map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	var path string
	var body string

	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, "fake-exiftool.cmd")
		body = `@echo off
if "%~1"=="-ver" (
  echo %FAKE_EXIFTOOL_VERSION%
  exit /b 0
)
if "%~1"=="-s3" (
  echo %FAKE_EXIFTOOL_MAJORBRAND%
  exit /b 0
)
if defined FAKE_EXIFTOOL_ARGS_FILE (
  >> "%FAKE_EXIFTOOL_ARGS_FILE%" echo %*
)
if "%~1"=="-j" (
  echo %FAKE_EXIFTOOL_JSON%
)
exit /b 0
`
	} else {
		path = filepath.Join(dir, "fake-exiftool")
		body = `#!/usr/bin/env sh
if [ "$1" = "-ver" ]; then
  printf '%s\n' "${FAKE_EXIFTOOL_VERSION}"
  exit 0
fi
if [ "$1" = "-s3" ]; then
  printf '%s\n' "${FAKE_EXIFTOOL_MAJORBRAND}"
  exit 0
fi
if [ -n "${FAKE_EXIFTOOL_ARGS_FILE}" ]; then
  printf '%s\n' "$*" >> "${FAKE_EXIFTOOL_ARGS_FILE}"
fi
if [ "$1" = "-j" ]; then
  printf '%s\n' "${FAKE_EXIFTOOL_JSON}"
fi
exit 0
`
	}

	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake exiftool: %v", err)
	}

	oldPath := exifToolPathOverride
	exifToolPathOverride = path
	t.Cleanup(func() {
		exifToolPathOverride = oldPath
	})

	defaultEnv := map[string]string{
		"FAKE_EXIFTOOL_VERSION":    "12.70",
		"FAKE_EXIFTOOL_MAJORBRAND": "",
		"FAKE_EXIFTOOL_JSON":       "[]",
	}
	for key, value := range defaultEnv {
		t.Setenv(key, value)
	}
	for key, value := range extraEnv {
		t.Setenv(key, value)
	}

	return path
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file %s: %v", path, err)
	}
	return string(data)
}

func requireContains(t *testing.T, body string, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("expected %q to contain %q", body, want)
	}
}
