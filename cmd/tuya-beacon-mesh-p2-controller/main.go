package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"example.com/tuya-beacon-mesh-p2-controller/internal/bluez"
	"example.com/tuya-beacon-mesh-p2-controller/internal/cli"
)

func run(args []string) error {
	if handled, err := bluez.DispatchHelper(args); handled {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Run(ctx, args)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
