package usecase

import "context"

// NotificationReplay prepares warning suppression only when replay needs
// enabling. Already-running replay is never interrupted for initial setup.
type NotificationReplay struct {
	client  ReplayClient
	prepare func(context.Context) error
}

func NewNotificationReplay(client ReplayClient, prepare func(context.Context) error) *NotificationReplay {
	return &NotificationReplay{client: client, prepare: prepare}
}

func (r *NotificationReplay) IsReplayEnabled(ctx context.Context) (bool, error) {
	return r.client.IsReplayEnabled(ctx)
}

func (r *NotificationReplay) EnableReplay(ctx context.Context) error {
	if err := r.prepare(ctx); err != nil {
		return err
	}
	return r.client.EnableReplay(ctx)
}
