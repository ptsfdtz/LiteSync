package main

import (
	"context"
	"io"
	"testing"

	"litesync/internal/api"
	"litesync/internal/logx"
)

type schedulerFake struct {
	calls int
}

func (s *schedulerFake) RegisterJob(context.Context, api.JobID) error   { return nil }
func (s *schedulerFake) UnregisterJob(context.Context, api.JobID) error { return nil }
func (s *schedulerFake) PushEvent(context.Context, api.FileEvent) error { return nil }
func (s *schedulerFake) Start(context.Context) error                    { return nil }
func (s *schedulerFake) Stop(context.Context) error                     { return nil }
func (s *schedulerFake) TriggerNow(_ context.Context, _ api.JobID, _ api.TriggerReason) (api.RunID, error) {
	s.calls++
	return "run-test", nil
}

func TestTriggerAllSync(t *testing.T) {
	fake := &schedulerFake{}
	logger := logx.NewWithWriter("debug", io.Discard)
	triggerAllSync(context.Background(), logger, fake, []api.JobID{"a", "b"})

	if fake.calls != 2 {
		t.Fatalf("expected 2 trigger calls, got %d", fake.calls)
	}
}
