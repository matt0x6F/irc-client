//go:build windows

package dcc

import "golang.org/x/sys/windows"

func availableDiskSpace(path string) (uint64, error) {
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var available uint64
	if err := windows.GetDiskFreeSpaceEx(wide, &available, nil, nil); err != nil {
		return 0, err
	}
	return available, nil
}
