package winapp

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInstancePreventsDuplicateAndReceivesStop(t *testing.T) {
	instance, err := AcquireInstance()
	if errors.Is(err, ErrAlreadyRunning) {
		t.Skip("a real monitor is already running")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(instance.Close)
	if running, err := IsRunning(); err != nil || !running {
		t.Fatalf("running=%v err=%v", running, err)
	}
	if duplicate, err := AcquireInstance(); !errors.Is(err, ErrAlreadyRunning) {
		if duplicate != nil {
			duplicate.Close()
		}
		t.Fatalf("duplicate: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	instance.WatchStop(cancel)
	stopped := make(chan error, 1)
	go func() { _, err := StopMonitor(); stopped <- err }()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("stop event not delivered")
	}
	instance.Close()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not finish")
	}
	if running, err := IsRunning(); err != nil || running {
		t.Fatalf("still running=%v err=%v", running, err)
	}
	if stopped, err := StopMonitor(); err != nil || stopped {
		t.Fatalf("idempotent stop=%v err=%v", stopped, err)
	}
}
