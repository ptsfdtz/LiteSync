package api

import "context"

type BackupManager interface {
	SyncNow(ctx context.Context, req SyncRequest) (SyncResult, error)
	SyncByEvents(ctx context.Context, jobID JobID, events []FileEvent, reason TriggerReason) (SyncResult, error)
	Reconcile(ctx context.Context, jobID JobID) (SyncResult, error)
	Cancel(ctx context.Context, runID RunID) error
}

type Watcher interface {
	Start(ctx context.Context, jobID JobID, sourceDir string) error
	Stop(ctx context.Context, jobID JobID) error
	Events() <-chan FileEvent
	Errors() <-chan error
}

type Scheduler interface {
	RegisterJob(ctx context.Context, jobID JobID) error
	UnregisterJob(ctx context.Context, jobID JobID) error
	PushEvent(ctx context.Context, event FileEvent) error
	TriggerNow(ctx context.Context, jobID JobID, reason TriggerReason) (RunID, error)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type ConfigService interface {
	Load(ctx context.Context) (Config, error)
	Save(ctx context.Context, cfg Config) error
	Validate(cfg Config) error
	Watch(ctx context.Context) (<-chan Config, error)
}

type StartupStatus struct {
	Enabled  bool
	Provider string
}

type StartupService interface {
	Enable(ctx context.Context) error
	Disable(ctx context.Context) error
	Status(ctx context.Context) (StartupStatus, error)
}

type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, err error, fields ...Field)
	With(fields ...Field) Logger
	Sync() error
}
