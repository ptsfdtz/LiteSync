package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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

	<-ctx.Done()
	logger.Info("LiteSync shutting down")
}

func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
	os.Exit(1)
}
