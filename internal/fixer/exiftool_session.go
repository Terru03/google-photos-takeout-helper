package fixer

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

type synchronizedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *synchronizedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

func (b *synchronizedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func (b *synchronizedBuffer) StringFrom(offset int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	data := b.buf.Bytes()
	if offset < 0 || offset > len(data) {
		offset = 0
	}
	return string(append([]byte(nil), data[offset:]...))
}

type exifToolSession struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	writer *bufio.Writer
	reader *bufio.Reader
	stderr synchronizedBuffer
	nextID uint64
	closed bool
}

var (
	exifSessionMu sync.RWMutex
	exifSession   *exifToolSession
)

func startExifToolSession(path string) (*exifToolSession, error) {
	cmd := newHiddenCommand(path, "-stay_open", "True", "-@", "-")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	session := &exifToolSession{
		cmd:    cmd,
		stdin:  stdin,
		writer: bufio.NewWriterSize(stdin, 64*1024),
		reader: bufio.NewReaderSize(stdout, 256*1024),
	}
	cmd.Stderr = &session.stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	return session, nil
}

func (s *exifToolSession) Run(args ...string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, fmt.Errorf("ExifTool session is closed")
	}
	s.nextID++
	id := strconv.FormatUint(s.nextID, 10)
	ready := "{ready" + id + "}"
	stderrOffset := s.stderr.Len()

	for _, arg := range args {
		if strings.ContainsAny(arg, "\r\n") {
			return nil, fmt.Errorf("ExifTool argument contains a newline")
		}
		if _, err := s.writer.WriteString(arg + "\n"); err != nil {
			return nil, err
		}
	}
	if _, err := s.writer.WriteString("-execute" + id + "\n"); err != nil {
		return nil, err
	}
	if err := s.writer.Flush(); err != nil {
		return nil, err
	}

	var output bytes.Buffer
	for {
		line, err := s.reader.ReadString('\n')
		if strings.TrimSpace(line) == ready {
			break
		}
		output.WriteString(line)
		if err != nil {
			return output.Bytes(), fmt.Errorf("ExifTool session ended before %s: %w", ready, err)
		}
	}

	stderrText := strings.TrimSpace(s.stderr.StringFrom(stderrOffset))
	if exifToolOutputHasError(stderrText) {
		return output.Bytes(), fmt.Errorf("%s", stderrText)
	}
	return output.Bytes(), nil
}

func (s *exifToolSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	var firstErr error
	if _, err := s.writer.WriteString("-stay_open\nFalse\n"); err != nil {
		firstErr = err
	}
	if err := s.writer.Flush(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.stdin.Close(); err != nil && firstErr == nil {
		firstErr = err
	}
	if err := s.cmd.Wait(); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func exifToolOutputHasError(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(line, "error:") ||
			strings.Contains(line, "files weren't updated due to errors") ||
			strings.Contains(line, "files weren't created due to errors") {
			return true
		}
	}
	return false
}

func activeExifToolSession() *exifToolSession {
	exifSessionMu.RLock()
	defer exifSessionMu.RUnlock()
	return exifSession
}
