package main

import (
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

	logger := logx.New(cfg.App.LogLevel)
	defer func() { _ = logger.Sync() }()

	backupManager := backup.New(logger)
	backupManager.ReplaceJobs(cfg.Jobs)
	schedulerSvc := scheduler.New(backupManager, logger)
	watcherSvc := watcher.New()
	startupSvc := startup.New()

	if err := schedulerSvc.Start(ctx); err != nil {
		fatal(err)
	}
	defer func() { _ = schedulerSvc.Stop(context.Background()) }()

	startupStatus, err := startupSvc.Status(ctx)
	if err != nil {
		logger.Warn("read startup status failed", api.Field{Key: "error", Value: err.Error()})
	}

	logger.Info(
		"LiteSync initialized",
		api.Field{Key: "config_path", Value: configPath},
		api.Field{Key: "jobs", Value: len(cfg.Jobs)},
		api.Field{Key: "startup_provider", Value: startupStatus.Provider},
		api.Field{Key: "startup_enabled", Value: startupStatus.Enabled},
		api.Field{Key: "watcher_stub", Value: fmt.Sprintf("%T", watcherSvc)},
	)

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

	<-ctx.Done()
	logger.Info("LiteSync shutting down")
}

func isInitialFullSync(job api.Job) bool {
	return strings.EqualFold(strings.TrimSpace(job.Strategy.InitialSync), "full")
}

func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
	os.Exit(1)
}
