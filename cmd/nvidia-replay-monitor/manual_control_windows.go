package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"nvidiareplaymonitor/internal/infra/manualcontrol"
)

func removeManualControlState() error {
	_, path, err := manualcontrol.DefaultPaths()
	if err != nil {
		return err
	}
	return manualcontrol.RemoveState(path)
}

func manualControlReport() (string, bool) {
	var report strings.Builder
	logPath, statePath, err := manualcontrol.DefaultPaths()
	if err != nil {
		return fmt.Sprintf("操作ログの場所を取得できません: %v\n", err), false
	}
	healthy := true
	fmt.Fprintf(&report, "操作ログ: %s\n", logPath)
	if f, err := os.Open(logPath); err == nil {
		report.WriteString("操作ログの読取: 可能\n")
		f.Close()
	} else {
		healthy = false
		fmt.Fprintf(&report, "操作ログの読取エラー: %v\n", err)
	}
	state, err := manualcontrol.ReadState(statePath)
	if errors.Is(err, os.ErrNotExist) {
		report.WriteString("手動操作の保存情報: 未作成\n")
		return report.String(), healthy
	}
	if err != nil {
		fmt.Fprintf(&report, "手動操作の保存情報エラー: %v\n", err)
		return report.String(), false
	}
	fmt.Fprintf(&report, "自動復帰の保留: %v\n", state.Paused)
	if state.Last != nil {
		fmt.Fprintf(&report, "最後の手動操作: %s オン=%v 経路=%s\n", state.Last.Time, state.Last.Enabled, state.Last.Source)
	}
	if state.ReadError != "" {
		fmt.Fprintf(&report, "監視中の操作ログエラー: %s\n", state.ReadError)
		healthy = false
	}
	return report.String(), healthy
}
