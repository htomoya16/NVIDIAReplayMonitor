package usecase

import (
	"context"
	"errors"
)

var ErrRestorePaused = errors.New("利用者の操作により自動復帰を保留しています")

type ControlledReplay struct {
	ReplayClient
	control RestoreControl
}

func NewControlledReplay(client ReplayClient, control RestoreControl) *ControlledReplay {
	return &ControlledReplay{ReplayClient: client, control: control}
}

func (c *ControlledReplay) EnableReplay(ctx context.Context) error {
	// Check again after status retrieval and notification setup: these can
	// take time, during which the user may have turned Instant Replay off.
	allowed, err := c.control.AllowRestore(ctx)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrRestorePaused
	}
	return c.ReplayClient.EnableReplay(ctx)
}
