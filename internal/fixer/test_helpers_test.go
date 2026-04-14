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
setlocal EnableDelayedExpansion
set "ARGS=%*"
if "%~1"=="-@" (
  set "ARGS="
  for /f "usebackq delims=" %%A in ("%~2") do (
    if defined ARGS (
      set "ARGS=!ARGS! %%A"
    ) else (
      set "ARGS=%%A"
    )
  )
)
echo !ARGS! | findstr /c:"-ver" >nul
if not errorlevel 1 (
  echo %FAKE_EXIFTOOL_VERSION%
  exit /b 0
)
echo !ARGS! | findstr /c:"-s3" >nul
if not errorlevel 1 (
  echo %FAKE_EXIFTOOL_MAJORBRAND%
  exit /b 0
)
if defined FAKE_EXIFTOOL_ARGS_FILE (
  >> "%FAKE_EXIFTOOL_ARGS_FILE%" echo !ARGS!
)
echo !ARGS! | findstr /c:"-j" >nul
if not errorlevel 1 (
  echo %FAKE_EXIFTOOL_JSON%
)
exit /b 0
`
	} else {
		path = filepath.Join(dir, "fake-exiftool")
		body = `#!/usr/bin/env sh
args="$*"
if [ "$1" = "-@" ] && [ -n "$2" ]; then
  args=""
  while IFS= read -r line || [ -n "$line" ]; do
    if [ -n "$args" ]; then
      args="$args $line"
    else
      args="$line"
    fi
  done < "$2"
fi
case " $args " in
  *" -ver "*)
    printf '%s\n' "${FAKE_EXIFTOOL_VERSION}"
    exit 0
    ;;
esac
case " $args " in
  *" -s3 "*)
    printf '%s\n' "${FAKE_EXIFTOOL_MAJORBRAND}"
    exit 0
    ;;
esac
if [ -n "${FAKE_EXIFTOOL_ARGS_FILE}" ]; then
  printf '%s\n' "$args" >> "${FAKE_EXIFTOOL_ARGS_FILE}"
fi
case " $args " in
  *" -j "*)
    printf '%s\n' "${FAKE_EXIFTOOL_JSON}"
    ;;
esac
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

func withFakeMotionPhotoTool(t *testing.T, extraEnv map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	var path string
	var body string

	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, "fake-motionphoto2.cmd")
		body = `@echo off
setlocal EnableDelayedExpansion
set "ARGS=%*"
if defined FAKE_MOTIONPHOTO_ARGS_FILE (
  >> "%FAKE_MOTIONPHOTO_ARGS_FILE%" echo !ARGS!
)
if defined FAKE_MOTIONPHOTO_APPEND_TO (
  >> "%FAKE_MOTIONPHOTO_APPEND_TO%" echo %FAKE_MOTIONPHOTO_APPEND_TEXT%
)
if defined FAKE_MOTIONPHOTO_STDOUT (
  echo %FAKE_MOTIONPHOTO_STDOUT%
)
exit /b %FAKE_MOTIONPHOTO_EXIT_CODE%
`
	} else {
		path = filepath.Join(dir, "fake-motionphoto2")
		body = `#!/usr/bin/env sh
args="$*"
if [ -n "${FAKE_MOTIONPHOTO_ARGS_FILE}" ]; then
  printf '%s\n' "$args" >> "${FAKE_MOTIONPHOTO_ARGS_FILE}"
fi
if [ -n "${FAKE_MOTIONPHOTO_APPEND_TO}" ]; then
  printf '%s\n' "${FAKE_MOTIONPHOTO_APPEND_TEXT}" >> "${FAKE_MOTIONPHOTO_APPEND_TO}"
fi
if [ -n "${FAKE_MOTIONPHOTO_STDOUT}" ]; then
  printf '%s\n' "${FAKE_MOTIONPHOTO_STDOUT}"
fi
exit "${FAKE_MOTIONPHOTO_EXIT_CODE}"
`
	}

	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake motionphoto2: %v", err)
	}

	oldPath := motionPhotoToolPathOverride
	motionPhotoToolPathOverride = path
	t.Cleanup(func() {
		motionPhotoToolPathOverride = oldPath
	})

	defaultEnv := map[string]string{
		"FAKE_MOTIONPHOTO_STDOUT":      "",
		"FAKE_MOTIONPHOTO_EXIT_CODE":   "0",
		"FAKE_MOTIONPHOTO_APPEND_TEXT": "embedded",
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

func requireContainsArg(t *testing.T, body string, flagName string, value string) {
	t.Helper()
	plain := flagName + " " + value
	quoted := flagName + ` "` + value + `"`
	if strings.Contains(body, plain) || strings.Contains(body, quoted) {
		return
	}
	t.Fatalf("expected %q to contain %q or %q", body, plain, quoted)
}
