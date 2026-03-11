package model

import "time"

type Config struct {
	SourceDir       string `json:"sourceDir"`
	TargetDir       string `json:"targetDir"`
	IntervalMinutes int    `json:"intervalMinutes"`
	WatchChanges    bool   `json:"watchChanges"`
}

func DefaultConfig() Config {
	return Config{
		IntervalMinutes: 60,
		WatchChanges:    false,
	}
}

type RuntimeStatus struct {
	Running          bool       `json:"running"`
	CurrentAction    string     `json:"currentAction,omitempty"`
	LastRunAt        *time.Time `json:"lastRunAt,omitempty"`
	LastSuccessAt    *time.Time `json:"lastSuccessAt,omitempty"`
	LastError        string     `json:"lastError,omitempty"`
	LastTrigger      string     `json:"lastTrigger,omitempty"`
	TotalRuns        int        `json:"totalRuns"`
	SuccessRuns      int        `json:"successRuns"`
	FailedRuns       int        `json:"failedRuns"`
	NextScheduledRun *time.Time `json:"nextScheduledRun,omitempty"`
}

type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}
