//go:build !windows

package tdl

import "os/exec"

func configureBackground(cmd *exec.Cmd) {}
