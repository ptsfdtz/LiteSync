package api

const (
	ConfigVersionV1 = 1

	DefaultLanguage                 = "zh-CN"
	DefaultRunMode                  = "tray"
	DefaultLogLevel                 = "info"
	DefaultJobMode                  = "mirror"
	DefaultInitialSync              = "full"
	DefaultEventDebounceMS          = 1500
	DefaultPeriodicReconcileEnabled = true
	DefaultPeriodicIntervalMinutes  = 30
	DefaultDeletePolicy             = "propagate"
	DefaultConflictPolicy           = "backup_then_overwrite"
	DefaultMaxParallelCopies        = 4
)

type Config struct {
	Version int       `yaml:"version"`
	App     AppConfig `yaml:"app"`
	Jobs    []Job     `yaml:"jobs"`
}

type AppConfig struct {
	Language string        `yaml:"language"`
	RunMode  string        `yaml:"run_mode"`
	LogLevel string        `yaml:"log_level"`
	LogDir   string        `yaml:"log_dir"`
	StateDir string        `yaml:"state_dir"`
	Startup  StartupConfig `yaml:"startup"`
}

type StartupConfig struct {
	Enabled bool `yaml:"enabled"`
}

type Job struct {
	ID        JobID    `yaml:"id"`
	Enabled   bool     `yaml:"enabled"`
	SourceDir string   `yaml:"source_dir"`
	TargetDir string   `yaml:"target_dir"`
	Exclude   []string `yaml:"exclude"`
	Strategy  Strategy `yaml:"strategy"`
}

type Strategy struct {
	Mode                string            `yaml:"mode"`
	InitialSync         string            `yaml:"initial_sync"`
	EventSync           EventSync         `yaml:"event_sync"`
	PeriodicReconcile   PeriodicReconcile `yaml:"periodic_reconcile"`
	DeletePolicy        string            `yaml:"delete_policy"`
	ConflictPolicy      string            `yaml:"conflict_policy"`
	MaxParallelCopies   int               `yaml:"max_parallel_copies"`
	FollowSymlinks      bool              `yaml:"follow_symlinks"`
	PreservePermissions bool              `yaml:"preserve_permissions"`
}

type EventSync struct {
	DebounceMS int `yaml:"debounce_ms"`
}

type PeriodicReconcile struct {
	Enabled         bool `yaml:"enabled"`
	IntervalMinutes int  `yaml:"interval_minutes"`
}

func DefaultConfig() Config {
	return Config{
		Version: ConfigVersionV1,
		App: AppConfig{
			Language: DefaultLanguage,
			RunMode:  DefaultRunMode,
			LogLevel: DefaultLogLevel,
			LogDir:   "",
			StateDir: "",
			Startup: StartupConfig{
				Enabled: true,
			},
		},
		Jobs: []Job{},
	}
}

func (c *Config) ApplyDefaults() {
	if c.Version == 0 {
		c.Version = ConfigVersionV1
	}
	if c.App.Language == "" {
		c.App.Language = DefaultLanguage
	}
	if c.App.RunMode == "" {
		c.App.RunMode = DefaultRunMode
	}
	if c.App.LogLevel == "" {
		c.App.LogLevel = DefaultLogLevel
	}

	for i := range c.Jobs {
		if c.Jobs[i].Strategy.Mode == "" {
			c.Jobs[i].Strategy.Mode = DefaultJobMode
		}
		if c.Jobs[i].Strategy.InitialSync == "" {
			c.Jobs[i].Strategy.InitialSync = DefaultInitialSync
		}
		if c.Jobs[i].Strategy.EventSync.DebounceMS == 0 {
			c.Jobs[i].Strategy.EventSync.DebounceMS = DefaultEventDebounceMS
		}
		if c.Jobs[i].Strategy.PeriodicReconcile.IntervalMinutes == 0 {
			c.Jobs[i].Strategy.PeriodicReconcile.IntervalMinutes = DefaultPeriodicIntervalMinutes
		}
		if c.Jobs[i].Strategy.DeletePolicy == "" {
			c.Jobs[i].Strategy.DeletePolicy = DefaultDeletePolicy
		}
		if c.Jobs[i].Strategy.ConflictPolicy == "" {
			c.Jobs[i].Strategy.ConflictPolicy = DefaultConflictPolicy
		}
		if c.Jobs[i].Strategy.MaxParallelCopies <= 0 {
			c.Jobs[i].Strategy.MaxParallelCopies = DefaultMaxParallelCopies
		}
		if c.Jobs[i].Exclude == nil {
			c.Jobs[i].Exclude = []string{}
		}
	}
}
