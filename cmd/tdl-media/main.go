package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/local/tdl-gui/internal/cli"
)

var version = "dev"

func main() {
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
