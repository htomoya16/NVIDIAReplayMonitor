package main

import "testing"

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
