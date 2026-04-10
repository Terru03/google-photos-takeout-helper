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

package fixer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type LogLevel string

const (
	LoggerInfo  LogLevel = "INFO"
	LoggerWarn  LogLevel = "WARN"
	LoggerError LogLevel = "ERROR"
)

var (
	logFileMu sync.Mutex
	logFile   *os.File

	logHandlerMu sync.RWMutex
	logHandler   func(level LogLevel, message string)
)

func SetLogHandler(handler func(level LogLevel, message string)) {
	logHandlerMu.Lock()
	defer logHandlerMu.Unlock()
	logHandler = handler
}

func getLogHandler() func(level LogLevel, message string) {
	logHandlerMu.RLock()
	defer logHandlerMu.RUnlock()
	return logHandler
}

func InitializeFileLogger(logDir string) (string, error) {
	logFileMu.Lock()
	defer logFileMu.Unlock()

	if logFile != nil {
		if err := logFile.Close(); err != nil {
			return "", err
		}
		logFile = nil
	}

	if logDir == "" {
		paths, err := ResolveRuntimePaths("")
		if err != nil {
			return "", err
		}
		logDir = paths.LogDir
	}

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return "", err
	}

	fileName := fmt.Sprintf("%s.txt", time.Now().Format("2006-01-02_15-04-05"))
	filePath := filepath.Join(logDir, fileName)

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", err
	}

	logFile = file
	return filePath, nil
}

func CloseFileLogger() error {
	logFileMu.Lock()
	defer logFileMu.Unlock()

	if logFile == nil {
		return nil
	}

	err := logFile.Close()
	logFile = nil
	return err
}

// Send a log message to the handler
func Log(level LogLevel, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	timestamp := time.Now().Format(time.RFC3339)
	logLine := fmt.Sprintf("[%s] [%s] %s\n", timestamp, level, msg)

	logFileMu.Lock()
	if logFile != nil {
		if _, err := logFile.WriteString(logLine); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write log file: %v\n", err)
		}
	}
	logFileMu.Unlock()

	if handler := getLogHandler(); handler != nil {
		handler(level, msg)
	}
}
