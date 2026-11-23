package app

import (
	"context"
	"fmt"
	"io"
	"os"

	"example.com/tuya-beacon-mesh-p2-controller/internal/controller"
)

// ExecuteCommand reports the same transmission details for CLI and bridge commands.
func ExecuteCommand(ctx context.Context, lamp controller.Controller, args []string) error {
	return executeCommand(ctx, lamp, args, os.Stdout, os.Stderr)
}

func executeCommand(ctx context.Context, lamp controller.Controller, args []string, out, warnings io.Writer) error {
	lamp.Prepared = func(result controller.Result) {
		for _, w := range result.Warnings {
			fmt.Fprintln(warnings, w)
		}
		frames := result.Frames
		if lamp.Options.DryRun {
			fmt.Fprintln(out, "DRY RUN: no Bluetooth transmission, no seq consumed")
		}
		fmt.Fprintf(out, "device=%s src=%04X dst=%04X frames=%d seq=%04X..%04X\n", lamp.DeviceID, lamp.Options.Src, lamp.Options.Dst, len(frames), frames[0].Seq, frames[len(frames)-1].Seq)
		if lamp.Options.DryRun {
			for _, fr := range frames {
				fmt.Fprintf(out, "seq=0x%04X %s frame=%x adv=%x\n", fr.Seq, fr.Label, fr.Frame, fr.Adv)
			}
		}
	}
	result, err := lamp.Execute(ctx, args)
	for _, w := range result.Warnings {
		fmt.Fprintln(warnings, w)
	}
	if err == nil && !lamp.Options.DryRun {
		fmt.Fprintf(out, "Done. Reserved and used seq 0x%04X..0x%04X; state=%s\n", result.Frames[0].Seq, result.Frames[len(result.Frames)-1].Seq, lamp.StatePath)
	}
	return err
}
