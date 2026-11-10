package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"nvidiareplaymonitor/internal/infra/notification"
	"nvidiareplaymonitor/internal/infra/nvidia"
	"nvidiareplaymonitor/internal/infra/winapp"
	"nvidiareplaymonitor/internal/usecase"
)

func buildDoctorReport() (string, bool) {
	var report strings.Builder
	fmt.Fprintf(&report, "NVIDIAReplayMonitor %s\n%s\nWindows %s\n\n", version, time.Now().Format(time.RFC3339), runtime.GOARCH)
	running, runErr := winapp.IsRunning()
	fmt.Fprintf(&report, "監視プロセス: %v\n", running)
	if runErr != nil {
		fmt.Fprintf(&report, "プロセス確認エラー: %v\n", runErr)
	}
	shortcut, shortcutErr := winapp.StartupShortcutPath()
	if shortcutErr == nil {
		_, statErr := os.Lstat(shortcut)
		fmt.Fprintf(&report, "自動起動ショートカット: %v\n場所: %s\n", statErr == nil, shortcut)
		if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			shortcutErr = statErr
			fmt.Fprintf(&report, "ショートカット確認エラー: %v\n", statErr)
		}
	} else {
		fmt.Fprintf(&report, "ショートカット確認エラー: %v\n", shortcutErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), usecase.CycleTimeout)
	manualReport, manualHealthy := manualControlReport()
	report.WriteString(manualReport)
	if manager, err := notification.DefaultManager(); err == nil {
		exists, stateErr := manager.HasState()
		fmt.Fprintf(&report, "警告設定の復元情報: %v\n場所: %s\n", exists, manager.StatePath())
		configured, configErr := manager.Configured()
		if configured {
			report.WriteString("警告抑止の初期設定: 完了\n")
		} else {
			report.WriteString("警告抑止の初期設定: 未完了（次の有効化前に自動設定）\n")
		}
		if configErr != nil {
			fmt.Fprintf(&report, "初期設定確認エラー: %v\n", configErr)
		}
		if stateErr != nil {
			fmt.Fprintf(&report, "復元情報確認エラー: %v\n", stateErr)
		}
	}
	defer cancel()
	client := nvidia.NewClient(version, nvidia.DiscoverEndpoint)
	defer client.Close()
	enabled, err := client.IsReplayEnabled(ctx)
	fmt.Fprintf(&report, "\nMCP接続先: %s\n", client.Endpoint())
	if err != nil {
		fmt.Fprintf(&report, "診断: 利用不可\n詳細: %v\n\nNVIDIA AppのMCPアクセスとIn-Game Overlayを確認してください。\n503の場合はMCPサーバーが利用不可のため、NVIDIA App側の状態を確認してください。\n", err)
		return report.String(), false
	}
	fmt.Fprintf(&report, "状態取得: 成功\nインスタントリプレイ: %v\n有効化ツール: %v\n", enabled, client.CanEnableReplay())
	if !client.CanEnableReplay() {
		report.WriteString("MCPのstandard以上のアクセス設定を確認してください。\n")
	}
	report.WriteString("\nこの診断はリプレイや録画設定を変更していません。\n")
	return report.String(), client.CanEnableReplay() && runErr == nil && shortcutErr == nil && manualHealthy
}
