package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"litesync/internal/api"
	"litesync/internal/backup"
	"litesync/internal/config"
	"litesync/internal/logx"
	"litesync/internal/scheduler"
	"litesync/internal/startup"
	"litesync/internal/state"
	"litesync/internal/watcher"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var configPath string
	var initConfigOnly bool
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.BoolVar(&initConfigOnly, "init-config", false, "write default config then exit")
	flag.Parse()

	if configPath == "" {
		defaultPath, err := config.DefaultPath()
		if err != nil {
			fatal(err)
		}
		configPath = defaultPath
	}

	cfgSvc := config.NewFileService(configPath)
	cfg, err := cfgSvc.Load(ctx)
	if err != nil {
		fatal(err)
	}

	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) || initConfigOnly {
		if err := cfgSvc.Save(ctx, cfg); err != nil {
			fatal(err)
		}
		fmt.Printf("config initialized at %s\n", configPath)
		if initConfigOnly {
			return
		}
	}

	logDir := strings.TrimSpace(cfg.App.LogDir)
	if logDir == "" {
		logDir, err = config.DefaultLogDir()
		if err != nil {
			fatal(err)
		}
	}
	stateDir := strings.TrimSpace(cfg.App.StateDir)
	if stateDir == "" {
		stateDir, err = config.DefaultStateDir()
		if err != nil {
			fatal(err)
		}
	}

	logger, logFile, err := logx.NewWithFile(cfg.App.LogLevel, logDir)
	if err != nil {
		fatal(err)
	}
	defer func() { _ = logger.Sync() }()

	backupManager := backup.New(logger)
	backupManager.ReplaceJobs(cfg.Jobs)
	schedulerSvc := scheduler.New(backupManager, logger)
	schedulerSvc.ConfigureJobs(cfg.Jobs)
	schedulerSvc.EnableRecovery(state.NewPendingEventStore(stateDir))
	watcherSvc := watcher.New()
	startupSvc := startup.New()
	stateStore := state.NewFileStore(stateDir)
	reporter := state.NewReportExporter(stateDir)

	if err := schedulerSvc.Start(ctx); err != nil {
		fatal(err)
	}

	startupStatus, err := startupSvc.Status(ctx)
	if err != nil {
		logger.Warn("read startup status failed", api.Field{Key: "error", Value: err.Error()})
	}
	if cfg.App.Startup.Enabled && !startupStatus.Enabled {
		if err := startupSvc.Enable(ctx); err != nil {
			logger.Warn("enable startup failed", api.Field{Key: "error", Value: err.Error()})
		}
	}
	if !cfg.App.Startup.Enabled && startupStatus.Enabled {
		if err := startupSvc.Disable(ctx); err != nil {
			logger.Warn("disable startup failed", api.Field{Key: "error", Value: err.Error()})
		}
	}
	startupStatus, err = startupSvc.Status(ctx)
	if err != nil {
		logger.Warn("refresh startup status failed", api.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(
		"LiteSync initialized",
		api.Field{Key: "config_path", Value: configPath},
		api.Field{Key: "jobs", Value: len(cfg.Jobs)},
		api.Field{Key: "log_dir", Value: logDir},
		api.Field{Key: "log_file", Value: logFile},
		api.Field{Key: "state_dir", Value: stateDir},
		api.Field{Key: "startup_provider", Value: startupStatus.Provider},
		api.Field{Key: "startup_enabled", Value: startupStatus.Enabled},
		api.Field{Key: "watcher_impl", Value: fmt.Sprintf("%T", watcherSvc)},
	)
	logger.Info("runtime commands available", api.Field{Key: "commands", Value: "help | sync | status | report | logs | open | exit"})

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-watcherSvc.Events():
				if err := schedulerSvc.PushEvent(ctx, event); err != nil {
					logger.Warn(
						"scheduler rejected file event",
						api.Field{Key: "job_id", Value: event.JobID},
						api.Field{Key: "path", Value: event.Path},
						api.Field{Key: "op", Value: event.Op},
						api.Field{Key: "error", Value: err.Error()},
					)
				}
			case err := <-watcherSvc.Errors():
				logger.Warn("watcher runtime error", api.Field{Key: "error", Value: err.Error()})
			}
		}
	}()

	activeJobs := make([]api.JobID, 0, len(cfg.Jobs))
	for _, job := range cfg.Jobs {
		if !job.Enabled {
			logger.Info("skip disabled job", api.Field{Key: "job_id", Value: job.ID})
			continue
		}

		if err := schedulerSvc.RegisterJob(ctx, job.ID); err != nil {
			logger.Error("register job failed", err, api.Field{Key: "job_id", Value: job.ID})
			continue
		}

		if err := watcherSvc.Start(ctx, job.ID, job.SourceDir); err != nil {
			logger.Error("start watcher failed", err, api.Field{Key: "job_id", Value: job.ID})
		}
		activeJobs = append(activeJobs, job.ID)

		if isInitialFullSync(job) {
			res, err := backupManager.SyncNow(ctx, api.SyncRequest{
				JobID:       job.ID,
				RequestID:   api.RequestID(fmt.Sprintf("startup-%d", time.Now().UnixNano())),
				Reason:      api.TriggerStartup,
				Mode:        api.SyncModeFull,
				RequestedAt: time.Now(),
			})
			if err != nil {
				logger.Error("startup full sync failed", err,
					api.Field{Key: "job_id", Value: job.ID},
					api.Field{Key: "run_id", Value: res.RunID},
					api.Field{Key: "errors", Value: res.ErrorCount},
				)
				continue
			}
			logger.Info("startup full sync success",
				api.Field{Key: "job_id", Value: job.ID},
				api.Field{Key: "run_id", Value: res.RunID},
				api.Field{Key: "copied", Value: res.CopiedFiles},
				api.Field{Key: "updated", Value: res.UpdatedFiles},
				api.Field{Key: "skipped", Value: res.SkippedFiles},
			)
		}
	}

	go commandLoop(ctx, stop, logger, schedulerSvc, backupManager, reporter, logDir, activeJobs)

	<-ctx.Done()
	logger.Info("LiteSync shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, jobID := range activeJobs {
		if err := watcherSvc.Stop(shutdownCtx, jobID); err != nil {
			logger.Warn("watcher stop failed", api.Field{Key: "job_id", Value: jobID}, api.Field{Key: "error", Value: err.Error()})
		}
	}
	if err := schedulerSvc.Stop(shutdownCtx); err != nil {
		logger.Warn("scheduler stop failed", api.Field{Key: "error", Value: err.Error()})
	}

	snapshot := backupManager.RuntimeSnapshot()
	if err := stateStore.Save(snapshot); err != nil {
		logger.Warn("save runtime state failed", api.Field{Key: "error", Value: err.Error()})
	} else {
		logger.Info("runtime state saved", api.Field{Key: "jobs", Value: len(snapshot.Jobs)}, api.Field{Key: "state_dir", Value: stateDir})
	}
}

func isInitialFullSync(job api.Job) bool {
	return strings.EqualFold(strings.TrimSpace(job.Strategy.InitialSync), "full")
}

func commandLoop(
	ctx context.Context,
	stop func(),
	logger api.Logger,
	schedulerSvc api.Scheduler,
	backupManager *backup.Manager,
	reporter *state.ReportExporter,
	logDir string,
	jobIDs []api.JobID,
) {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !scanner.Scan() {
			return
		}
		cmd := strings.ToLower(strings.TrimSpace(scanner.Text()))
		switch cmd {
		case "":
			continue
		case "sync":
			triggerAllSync(ctx, logger, schedulerSvc, jobIDs)
		case "help":
			logger.Info("available commands", api.Field{Key: "commands", Value: "help | sync | status | report | logs | open | exit"})
		case "status":
			summary := backupManager.RuntimeSummary()
			logger.Info(
				"runtime status",
				api.Field{Key: "jobs", Value: summary.JobCount},
				api.Field{Key: "copied_total", Value: summary.Totals.CopiedFiles},
				api.Field{Key: "updated_total", Value: summary.Totals.UpdatedFiles},
				api.Field{Key: "deleted_total", Value: summary.Totals.DeletedFiles},
				api.Field{Key: "conflicts_total", Value: summary.Totals.ConflictCount},
				api.Field{Key: "errors_total", Value: summary.Totals.ErrorCount},
				api.Field{Key: "error_codes", Value: fmt.Sprintf("%v", summary.ErrorCodes)},
			)
		case "report":
			summary := backupManager.RuntimeSummary()
			snapshot := backupManager.RuntimeSnapshot()
			path, err := reporter.Export(summary, snapshot)
			if err != nil {
				logger.Warn("export runtime report failed", api.Field{Key: "error", Value: err.Error()})
				continue
			}
			logger.Info("runtime report exported", api.Field{Key: "path", Value: path})
		case "logs":
			logger.Info("log directory", api.Field{Key: "path", Value: logDir})
		case "open":
			logger.Info("open command received", api.Field{Key: "status", Value: "UI not implemented, command acknowledged"})
		case "exit", "quit":
			logger.Info("exit command received")
			stop()
			return
		default:
			logger.Warn("unknown command", api.Field{Key: "command", Value: cmd})
		}
	}
}

func triggerAllSync(ctx context.Context, logger api.Logger, schedulerSvc api.Scheduler, jobIDs []api.JobID) {
	for _, jobID := range jobIDs {
		runID, err := schedulerSvc.TriggerNow(ctx, jobID, api.TriggerManual)
		if err != nil {
			logger.Warn("manual sync trigger failed", api.Field{Key: "job_id", Value: jobID}, api.Field{Key: "error", Value: err.Error()})
			continue
		}
		logger.Info("manual sync triggered", api.Field{Key: "job_id", Value: jobID}, api.Field{Key: "run_id", Value: runID})
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
	os.Exit(1)
}
