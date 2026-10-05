package tdl

import (
	"os/exec"
	"syscall"
)

func configureBackground(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// Piped engine calls must not allocate a console when launched by the GUI worker.
	cmd.SysProcAttr.CreationFlags |= 0x08000000 // CREATE_NO_WINDOW
}
