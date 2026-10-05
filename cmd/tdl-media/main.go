package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/local/tdl-gui/internal/cli"
	"github.com/local/tdl-gui/internal/updates"
)

var version = "dev"

func main() {
	if len(os.Args) == 3 && os.Args[1] == "internal-apply-update" {
		if err := updates.RunHelper(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	command, cleanup := cli.NewWithCleanup(version)
	err := command.ExecuteContext(ctx)
	if closeErr := cleanup(); err == nil {
		err = closeErr
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}
