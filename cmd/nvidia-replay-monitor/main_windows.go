package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"nvidiareplaymonitor/internal/infra/manualcontrol"
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
	logPath, statePath, err := manualcontrol.DefaultPaths()
	if err != nil {
		showMessage(fmt.Sprintf("操作ログの場所を取得できません: %v", err), true)
		return 1
	}
	control := manualcontrol.New(logPath, statePath)
	prepare := warningInitializer(client)
	// With Overlay already off, setup can safely run without MCP. This also
	// permits the manual fallback before the NVIDIA app is turned back on.
	if allowed, err := control.AllowRestore(ctx); err == nil && allowed {
		if running, err := winapp.OverlayRunning(); err == nil && !running {
			_ = prepare(ctx)
		}
	}
	guardedClient := usecase.NewControlledReplay(client, control)
	usecase.NewControlledMonitor(usecase.NewNotificationReplay(guardedClient, prepare), control).Run(ctx)
	return 0
}
