package main

import (
	"context"
	"fmt"
	"time"

	"nvidiareplaymonitor/internal/infra/notification"
	"nvidiareplaymonitor/internal/infra/nvidia"
	"nvidiareplaymonitor/internal/infra/winapp"
)

func restoreWarning() error { return configureWarning(context.Background(), true) }

func configureWarning(parent context.Context, restore bool) error {
	manager, err := notification.DefaultManager()
	if err != nil {
		return err
	}
	if restore {
		exists, err := manager.HasState()
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		needsOverlay, err := manager.NeedsOverlayForRestore()
		if err != nil {
			return err
		}
		if !needsOverlay {
			return manager.Restore()
		}
	}
	ctx, cancel := context.WithTimeout(parent, time.Minute)
	defer cancel()
	change := manager.Apply
	if restore {
		change = manager.Restore
	}
	idle := func(ctx context.Context) error {
		client := nvidia.NewClient(version, nvidia.DiscoverEndpoint)
		defer client.Close()
		return client.CanRestartOverlay(ctx)
	}
	return winapp.WithOverlayStopped(ctx, idle, waitForOverlayMCP, change)
}

func warningInitializer(client *nvidia.Client) func(context.Context) error {
	configured := false
	return func(ctx context.Context) error {
		if configured {
			return nil
		}
		manager, err := notification.DefaultManager()
		if err != nil {
			return err
		}
		configured, err = manager.Configured()
		if err != nil || configured {
			return err
		}
		release, err := winapp.AcquireMaintenance()
		if err != nil {
			return err
		}
		defer release()
		// Overlay restart invalidates the monitor's existing MCP session.
		client.Close()
		if err = configureWarning(ctx, false); err != nil {
			return err
		}
		configured = true
		return nil
	}
}

func waitForOverlayMCP(ctx context.Context) error {
	var lastErr error
	for {
		client := nvidia.NewClient(version, nvidia.DiscoverEndpoint)
		_, lastErr = client.IsReplayEnabled(ctx)
		client.Close()
		if lastErr == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Overlay再起動後のMCP準備を確認できません。NVIDIA Appの状態を確認してください: %w", lastErr)
		case <-time.After(time.Second):
		}
	}
}
