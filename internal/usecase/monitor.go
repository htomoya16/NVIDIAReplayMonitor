package usecase

import (
	"context"
	"time"
)

const (
	Interval     = 30 * time.Second
	CycleTimeout = 20 * time.Second
)

// ReplayClient returns an error when the replay state cannot be determined.
type ReplayClient interface {
	IsReplayEnabled(context.Context) (bool, error)
	EnableReplay(context.Context) error
}

type Monitor struct{ client ReplayClient }

func NewMonitor(client ReplayClient) *Monitor { return &Monitor{client: client} }

func (m *Monitor) Run(ctx context.Context) {
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()
	m.runOnTicks(ctx, ticker.C)
}

func (m *Monitor) runOnTicks(ctx context.Context, ticks <-chan time.Time) {
	for ctx.Err() == nil {
		// Every failure waits for the next scheduled tick; no immediate retries.
		_ = m.restoreReplayIfDisabled(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
	}
}

func (m *Monitor) restoreReplayIfDisabled(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, CycleTimeout)
	defer cancel()
	enabled, err := m.client.IsReplayEnabled(ctx)
	if err != nil {
		return err
	}
	if enabled {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.client.EnableReplay(ctx)
}
