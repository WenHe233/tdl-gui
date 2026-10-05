package jobs

import "golang.org/x/sys/windows"

func publishFile(src, dst string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	// No REPLACE_EXISTING flag: a racing writer must never be overwritten.
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}
