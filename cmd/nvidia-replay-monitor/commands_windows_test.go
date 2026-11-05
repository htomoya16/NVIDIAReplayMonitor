package main

import (
	"errors"
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
	if err := uninstall(stop, restore, remove); err == nil {
		t.Fatal("failure hidden")
	}
	if len(calls) != 2 || calls[0] != "stop" || calls[1] != "restore" {
		t.Fatalf("calls: %v", calls)
	}
}
