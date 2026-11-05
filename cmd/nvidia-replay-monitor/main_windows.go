package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"nvidiareplaymonitor/internal/infra/nvidia"
	"nvidiareplaymonitor/internal/infra/winapp"
	"nvidiareplaymonitor/internal/usecase"
)

var version = "dev"

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	opts, err := parseOptions(args)
	if err != nil {
		showMessage(err.Error(), true)
		return 2
	}
	if handled, code := runCommand(opts); handled {
		return code
	}
	release, err := winapp.AcquireMaintenance()
	if err != nil {
		showMessage(err.Error(), true)
		return 1
	}
	instance, err := winapp.AcquireInstance()
	release()
	if errors.Is(err, winapp.ErrAlreadyRunning) {
		return 0
	}
	if err != nil {
		showMessage(fmt.Sprintf("監視を開始できません: %v", err), true)
		return 1
	}
	defer instance.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	instance.WatchStop(cancel)
	client := nvidia.NewClient(version, nvidia.DiscoverEndpoint)
	defer client.Close()
	prepare := warningInitializer(client)
	// With Overlay already off, setup can safely run without MCP. This also
	// permits the manual fallback before the NVIDIA app is turned back on.
	if running, err := winapp.OverlayRunning(); err == nil && !running {
		_ = prepare(ctx)
	}
	usecase.NewMonitor(usecase.NewNotificationReplay(client, prepare)).Run(ctx)
	return 0
}
