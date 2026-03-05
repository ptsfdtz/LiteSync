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

type JobRuntimeState struct {
	JobID         JobID     `json:"job_id"`
	LastRunID     RunID     `json:"last_run_id"`
	LastReason    string    `json:"last_reason"`
	LastErrorCode string    `json:"last_error_code"`
	StartedAt     time.Time `json:"started_at"`
	FinishedAt    time.Time `json:"finished_at"`
	CopiedFiles   uint64    `json:"copied_files"`
	UpdatedFiles  uint64    `json:"updated_files"`
	DeletedFiles  uint64    `json:"deleted_files"`
	SkippedFiles  uint64    `json:"skipped_files"`
	ConflictCount uint64    `json:"conflict_count"`
	ErrorCount    uint64    `json:"error_count"`
}

type RuntimeSnapshot struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Jobs        []JobRuntimeState `json:"jobs"`
}

type RuntimeSummary struct {
	GeneratedAt time.Time         `json:"generated_at"`
	JobCount    int               `json:"job_count"`
	Totals      SyncResult        `json:"totals"`
	ErrorCodes  map[string]uint64 `json:"error_codes"`
}
