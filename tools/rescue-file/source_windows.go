//go:build windows

package main

import (
	"fmt"
	"io"
	"time"

	"golang.org/x/sys/windows"
)

type windowsRescueSource struct {
	handle  windows.Handle
	timeout time.Duration
}

func openRescueSource(path string, timeout time.Duration) (sourceReader, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OVERLAPPED|windows.FILE_FLAG_RANDOM_ACCESS,
		0,
	)
	if err != nil {
		return nil, err
	}
	return &windowsRescueSource{handle: handle, timeout: timeout}, nil
}

func (source *windowsRescueSource) Close() error {
	return windows.CloseHandle(source.handle)
}

func (source *windowsRescueSource) ReadAt(buffer []byte, offset int64) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}

	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(event)

	overlapped := windows.Overlapped{
		Offset:     uint32(offset),
		OffsetHigh: uint32(uint64(offset) >> 32),
		HEvent:     event,
	}
	var done uint32
	err = windows.ReadFile(source.handle, buffer, &done, &overlapped)
	if err == nil {
		if int(done) != len(buffer) {
			return int(done), io.ErrUnexpectedEOF
		}
		return int(done), nil
	}
	if err != windows.ERROR_IO_PENDING {
		return int(done), err
	}

	waitMilliseconds := uint32(source.timeout / time.Millisecond)
	if waitMilliseconds == 0 {
		waitMilliseconds = 1
	}
	waitResult, waitErr := windows.WaitForSingleObject(event, waitMilliseconds)
	if waitErr != nil {
		return int(done), waitErr
	}
	if waitResult == windows.WAIT_OBJECT_0 {
		err = windows.GetOverlappedResult(source.handle, &overlapped, &done, false)
		if err != nil {
			return int(done), err
		}
		if int(done) != len(buffer) {
			return int(done), io.ErrUnexpectedEOF
		}
		return int(done), nil
	}
	if waitResult != uint32(windows.WAIT_TIMEOUT) {
		return int(done), fmt.Errorf("unexpected wait result: %d", waitResult)
	}

	cancelErr := windows.CancelIoEx(source.handle, &overlapped)
	if cancelErr != nil && cancelErr != windows.ERROR_NOT_FOUND {
		return int(done), fmt.Errorf("read timeout and cancel failed: %w", cancelErr)
	}
	_, _ = windows.WaitForSingleObject(event, windows.INFINITE)
	resultErr := windows.GetOverlappedResult(source.handle, &overlapped, &done, false)
	if resultErr == nil && int(done) == len(buffer) {
		return int(done), nil
	}
	return int(done), fmt.Errorf("read timeout after %s", source.timeout)
}
