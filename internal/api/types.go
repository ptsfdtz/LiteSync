package api

import "time"

type JobID string
type RunID string
type RequestID string

type TriggerReason string

const (
	TriggerStartup   TriggerReason = "startup"
	TriggerFileEvent TriggerReason = "file_event"
	TriggerSchedule  TriggerReason = "schedule"
	TriggerManual    TriggerReason = "manual"
	TriggerReconcile TriggerReason = "reconcile"
)

type SyncMode string

const (
	SyncModeFull        SyncMode = "full"
	SyncModeIncremental SyncMode = "incremental"
)

type FileOp string

const (
	FileCreate FileOp = "create"
	FileWrite  FileOp = "write"
	FileRemove FileOp = "remove"
	FileRename FileOp = "rename"
	FileChmod  FileOp = "chmod"
)

type SyncRequest struct {
	JobID        JobID
	RequestID    RequestID
	Reason       TriggerReason
	Mode         SyncMode
	ChangedPaths []string
	Force        bool
	RequestedAt  time.Time
}

type SyncResult struct {
	JobID         JobID
	RunID         RunID
	StartedAt     time.Time
	FinishedAt    time.Time
	CopiedFiles   uint64
	UpdatedFiles  uint64
	DeletedFiles  uint64
	SkippedFiles  uint64
	ConflictCount uint64
	ErrorCount    uint64
}

type FileEvent struct {
	JobID      JobID
	Path       string
	Op         FileOp
	IsDir      bool
	OccurredAt time.Time
}

type Field struct {
	Key   string
	Value any
}
