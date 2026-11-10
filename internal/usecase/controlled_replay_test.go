package usecase

import (
	"context"
	"errors"
	"testing"
)

type controlStub func(context.Context) (bool, error)

func (c controlStub) AllowRestore(ctx context.Context) (bool, error) { return c(ctx) }

func TestPausedMonitorKeepsPollingIntentWithoutCapture(t *testing.T) {
	allow := false
	reads, enables := 0, 0
	client := replayStub{
		read:   func(context.Context) (bool, error) { reads++; return false, nil },
		enable: func(context.Context) error { enables++; return nil },
	}
	control := controlStub(func(context.Context) (bool, error) { return allow, nil })
	m := NewControlledMonitor(NewControlledReplay(client, control), control)
	for i := 0; i < 3; i++ {
		if err := m.restoreReplayIfDisabled(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if reads != 0 || enables != 0 {
		t.Fatal("paused monitor touched capture")
	}
	allow = true
	if err := m.restoreReplayIfDisabled(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reads != 1 || enables != 1 {
		t.Fatalf("reads=%d enables=%d", reads, enables)
	}
}

func TestLateManualOffDuringSetupPreventsEnable(t *testing.T) {
	allow := true
	control := controlStub(func(context.Context) (bool, error) { return allow, nil })
	client := replayStub{
		read:   func(context.Context) (bool, error) { return false, nil },
		enable: func(context.Context) error { t.Fatal("manual off was overwritten"); return nil },
	}
	prepare := func(context.Context) error { allow = false; return nil }
	m := NewControlledMonitor(NewNotificationReplay(NewControlledReplay(client, control), prepare), control)
	if err := m.restoreReplayIfDisabled(context.Background()); !errors.Is(err, ErrRestorePaused) {
		t.Fatalf("err=%v", err)
	}
}

func TestControlReadFailureDoesNotEnable(t *testing.T) {
	control := controlStub(func(context.Context) (bool, error) { return false, errors.New("unreadable log") })
	client := replayStub{
		read: func(context.Context) (bool, error) { t.Fatal("capture state read after log error"); return false, nil },
	}
	if err := NewControlledMonitor(client, control).restoreReplayIfDisabled(context.Background()); err == nil {
		t.Fatal("error hidden")
	}
}
