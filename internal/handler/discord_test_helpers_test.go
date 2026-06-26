package handler

import (
	"context"

	"github.com/openvibely/openvibely/internal/service"
)

type fakeDiscordService struct {
	statusFn         func(ctx context.Context) (service.DiscordConnectionStatus, error)
	disconnectFn     func(ctx context.Context) error
	reloadFn         func(ctx context.Context) error
	testFn           func(ctx context.Context) error
	taskCompletionFn func(ctx context.Context, channelID, threadID, messageID, taskTitle, output, errMsg, userID string)
}

func (f *fakeDiscordService) GetConnectionStatus(ctx context.Context) (service.DiscordConnectionStatus, error) {
	if f != nil && f.statusFn != nil {
		return f.statusFn(ctx)
	}
	return service.DiscordConnectionStatus{}, nil
}

func (f *fakeDiscordService) Disconnect(ctx context.Context) error {
	if f != nil && f.disconnectFn != nil {
		return f.disconnectFn(ctx)
	}
	return nil
}

func (f *fakeDiscordService) ReloadFromSettings(ctx context.Context) error {
	if f != nil && f.reloadFn != nil {
		return f.reloadFn(ctx)
	}
	return nil
}

func (f *fakeDiscordService) TestConnection(ctx context.Context) error {
	if f != nil && f.testFn != nil {
		return f.testFn(ctx)
	}
	return nil
}

func (f *fakeDiscordService) SendTaskCompletionToThread(ctx context.Context, channelID, threadID, messageID, taskTitle, output, errMsg, userID string) {
	if f != nil && f.taskCompletionFn != nil {
		f.taskCompletionFn(ctx, channelID, threadID, messageID, taskTitle, output, errMsg, userID)
	}
}
