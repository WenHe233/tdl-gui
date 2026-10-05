//go:build !windows

package updates

import (
	"context"
	"errors"
	"os/exec"
)

func background(cmd *exec.Cmd) {}
func waitProcess(ctx context.Context, pid int) error {
	return errors.New("portable updates require Windows")
}
