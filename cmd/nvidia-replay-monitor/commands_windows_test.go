package main

import (
	"errors"
	"nvidiareplaymonitor/internal/infra/winapp"
	"testing"
)

func TestCommandOptionsRejectAmbiguousActions(t *testing.T) {
	for _, args := range [][]string{{"--stop", "--doctor"}, {"--uninstall", "--version"}, {"--output", "x"}, {"--diagnose"}, {"unknown"}} {
		if _, err := parseOptions(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	for _, args := range [][]string{nil, {"--stop"}, {"--uninstall"}, {"--doctor"}, {"--doctor", "--output", "doctor.txt"}, {"--help"}} {
		if _, err := parseOptions(args); err != nil {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestUninstallStopsBeforeRestoreAndKeepsStartupOnFailure(t *testing.T) {
	var calls []string
	stop := func() (bool, error) { calls = append(calls, "stop"); return true, nil }
	restore := func() error { calls = append(calls, "restore"); return errors.New("busy") }
	remove := func() (bool, error) { calls = append(calls, "remove"); return true, nil }
	if _, err := uninstall(stop, restore, remove); err == nil {
		t.Fatal("failure hidden")
	}
	if len(calls) != 2 || calls[0] != "stop" || calls[1] != "restore" {
		t.Fatalf("calls: %v", calls)
	}
}

func TestUninstallCompletesAfterRestorationDespiteRestartWarning(t *testing.T) {
	removed := false
	stop := func() (bool, error) { return true, nil }
	restore := func() error { return &winapp.OverlayRestartWarning{Cause: errors.New("MCP not ready")} }
	remove := func() (bool, error) { removed = true; return true, nil }
	warning, err := uninstall(stop, restore, remove)
	if err != nil || warning == nil || !removed {
		t.Fatalf("warning=%v err=%v removed=%v", warning, err, removed)
	}
}
