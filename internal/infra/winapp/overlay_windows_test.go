package winapp

import (
	"errors"
	"testing"
)

func TestRestartTargetIdentityAndSafety(t *testing.T) {
	expected := restartIdentity{PID: 123}
	for _, test := range []struct {
		name                              string
		code                              uintptr
		count, needed, reasons, kind, pid uint32
		safe                              bool
	}{
		{"main-window", 0, 1, 1, 0, 1, 123, true},
		{"other-window", 0, 1, 1, 0, 2, 123, true},
		{"no-window-classification", 0, 1, 1, 0, 0, 123, true},
		{"no-process", 0, 0, 0, 0, 1, 123, false},
		{"changed-list", 234, 1, 2, 0, 1, 123, false},
		{"reboot-required", 0, 1, 1, 1, 1, 123, false},
		{"wrong-pid", 0, 1, 1, 0, 1, 999, false},
		{"service", 0, 1, 1, 0, 3, 123, false},
		{"critical", 0, 1, 1, 0, 1000, 123, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := restartProcessInfo{Process: restartIdentity{PID: test.pid}, AppType: test.kind}
			err := validateRestartTarget(test.code, test.count, test.needed, test.reasons, info, expected)
			if (err == nil) != test.safe {
				t.Fatalf("safe=%v error=%v", test.safe, err)
			}
		})
	}
}

func TestRestartFailureCannotHideRestorationFailure(t *testing.T) {
	restoreFailure, restartFailure := errors.New("restore failed"), errors.New("restart failed")
	var notice *OverlayRestartWarning
	if err := operationResult(restoreFailure, restartFailure); !errors.Is(err, restoreFailure) || errors.As(err, &notice) {
		t.Fatalf("restoration failure treated as warning: %v", err)
	}
	if err := operationResult(nil, restartFailure); !errors.As(err, &notice) {
		t.Fatalf("completed restoration not identified: %v", err)
	}
}
