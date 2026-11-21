package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"example.com/tuya-beacon-mesh-p2-controller/internal/app"
	"example.com/tuya-beacon-mesh-p2-controller/internal/bluez"
	"example.com/tuya-beacon-mesh-p2-controller/internal/config"
)

func run(args []string) error {
	if handled, err := bluez.DispatchHelper(args); handled {
		return err
	}
	flags := flag.NewFlagSet("tuya-beacon-mesh-p2-controller-server", flag.ContinueOnError)
	path := flags.String("config", config.DefaultPath(), "configuration file")
	statePath := flags.String("state", "", "override the configured state file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return app.RunServer(ctx, *path, *statePath)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
