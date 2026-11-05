package usecase

import (
	"context"
	"errors"
	"testing"
	"time"
)

type replayStub struct {
	read   func(context.Context) (bool, error)
	enable func(context.Context) error
}

func (s replayStub) IsReplayEnabled(ctx context.Context) (bool, error) { return s.read(ctx) }
func (s replayStub) EnableReplay(ctx context.Context) error            { return s.enable(ctx) }

func TestRestoreReplayOnlyWhenExplicitlyDisabled(t *testing.T) {
	failure := errors.New("NVIDIA unavailable")
	for _, test := range []struct {
		name               string
		enabled            bool
		readErr, enableErr error
		wantCalls          int
		wantErr            bool
	}{
		{name: "already enabled", enabled: true},
		{name: "disabled", wantCalls: 1},
		{name: "unknown state", readErr: failure, wantErr: true},
		{name: "enable rejected", enableErr: failure, wantCalls: 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := replayStub{
				read: func(ctx context.Context) (bool, error) {
					deadline, ok := ctx.Deadline()
					if !ok || time.Until(deadline) > CycleTimeout {
						t.Error("cycle deadline missing")
					}
					return test.enabled, test.readErr
				},
				enable: func(context.Context) error { calls++; return test.enableErr },
			}
			err := NewMonitor(client).restoreReplayIfDisabled(context.Background())
			if (err != nil) != test.wantErr || calls != test.wantCalls {
				t.Fatalf("err=%v enable calls=%d", err, calls)
			}
		})
	}
}

func TestMonitorWaitsForNextTickAfterFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ticks := make(chan time.Time)
	reads := make(chan struct{}, 3)
	enables := make(chan struct{}, 3)
	readCount := 0
	client := replayStub{
		read: func(context.Context) (bool, error) {
			readCount++
			reads <- struct{}{}
			if readCount == 1 {
				return false, errors.New("connection refused")
			}
			return false, nil
		},
		enable: func(context.Context) error { enables <- struct{}{}; return errors.New("DRM restriction") },
	}
	done := make(chan struct{})
	go func() { NewMonitor(client).runOnTicks(ctx, ticks); close(done) }()
	waitEvent(t, reads)
	select {
	case <-enables:
		t.Fatal("unknown state triggered capture")
	default:
	}
	// An unbuffered send synchronizes with the scheduler's wait, proving that
	// the previous failure did not cause an immediate retry loop.
	ticks <- time.Now()
	waitEvent(t, reads)
	waitEvent(t, enables)
	ticks <- time.Now()
	waitEvent(t, reads)
	waitEvent(t, enables)
	cancel()
	waitEvent(t, done)
	if readCount != 3 {
		t.Fatalf("read count=%d", readCount)
	}
}

func TestStopCancelsInFlightReadWithoutEnabling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	client := replayStub{
		read:   func(ctx context.Context) (bool, error) { close(started); <-ctx.Done(); return false, ctx.Err() },
		enable: func(context.Context) error { t.Error("enable called after cancellation"); return nil },
	}
	done := make(chan struct{})
	go func() { NewMonitor(client).runOnTicks(ctx, make(chan time.Time)); close(done) }()
	waitEvent(t, started)
	cancel()
	waitEvent(t, done)
}

func waitEvent(t *testing.T, event <-chan struct{}) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for monitor")
	}
}
