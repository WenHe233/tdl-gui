package updates

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"os/exec"
	"syscall"
)

func background(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} }
func waitProcess(ctx context.Context, pid int) error {
	if pid <= 0 {
		return errors.New("更新进程 ID 无效")
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		state, err := windows.WaitForSingleObject(handle, 100)
		if err != nil {
			return err
		}
		if state == windows.WAIT_OBJECT_0 {
			return nil
		}
	}
}
