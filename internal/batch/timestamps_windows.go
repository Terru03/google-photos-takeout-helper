//go:build windows

package batch

import "golang.org/x/sys/windows"

func preserveFileTimestamps(sourcePath string, targetPath string) error {
	sourceName, err := windows.UTF16PtrFromString(sourcePath)
	if err != nil {
		return err
	}
	targetName, err := windows.UTF16PtrFromString(targetPath)
	if err != nil {
		return err
	}
	share := uint32(windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE)
	source, err := windows.CreateFile(
		sourceName,
		windows.FILE_READ_ATTRIBUTES,
		share,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return err
	}
	defer func() {
		_ = windows.CloseHandle(source)
	}()
	var created windows.Filetime
	var accessed windows.Filetime
	var modified windows.Filetime
	if err := windows.GetFileTime(source, &created, &accessed, &modified); err != nil {
		return err
	}

	target, err := windows.CreateFile(
		targetName,
		windows.FILE_WRITE_ATTRIBUTES,
		share,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return err
	}
	defer func() {
		_ = windows.CloseHandle(target)
	}()
	return windows.SetFileTime(target, &created, &accessed, &modified)
}
