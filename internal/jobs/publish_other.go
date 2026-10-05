//go:build !windows

package jobs

import "os"

func publishFile(src, dst string) error { return os.Link(src, dst) }
