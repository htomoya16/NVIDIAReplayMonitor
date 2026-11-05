package usecase

import (
	"context"
	"errors"
	"testing"
)

func TestWarningPreparationGuardsCapture(t *testing.T) {
	for _, test := range []struct {
		name                    string
		on, prepareFails        bool
		wantPrepare, wantEnable int
	}{
		{"already-on", true, false, 0, 0},
		{"off", false, false, 1, 1},
		{"initialization-unavailable", false, true, 1, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared, enabled := 0, 0
			client := replayStub{read: func(context.Context) (bool, error) { return test.on, nil }, enable: func(context.Context) error { enabled++; return nil }}
			prepare := func(context.Context) error {
				prepared++
				if test.prepareFails {
					return errors.New("setup unavailable")
				}
				return nil
			}
			err := NewMonitor(NewNotificationReplay(client, prepare)).restoreReplayIfDisabled(context.Background())
			if (err != nil) != test.prepareFails || prepared != test.wantPrepare || enabled != test.wantEnable {
				t.Fatalf("err=%v prepared=%d enabled=%d", err, prepared, enabled)
			}
		})
	}
}
