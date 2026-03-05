//go:build cgo

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

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
	var configPath string
	flag.StringVar(&configPath, "config", "", "path to YAML config file")
	flag.Parse()

	if configPath == "" {
		defaultPath, err := config.DefaultPath()
		if err != nil {
			fatal(err)
		}
		configPath = defaultPath
	}

	appUI := app.NewWithID("io.litesync.gui")
	win := appUI.NewWindow("LiteSync")
	win.Resize(fyne.NewSize(980, 700))

	cfgSvc := config.NewFileService(configPath)
	ctx := context.Background()
	cfg, err := ensureConfig(ctx, cfgSvc)
	if err != nil {
		dialog.ShowError(err, win)
		cfg = api.DefaultConfig()
	}

	var logMu sync.Mutex
	logLines := make([]string, 0, 256)
	logView := widget.NewMultiLineEntry()
	logView.SetMinRowsVisible(16)
	logView.Wrapping = fyne.TextWrapWord

	appendLog := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		entry := fmt.Sprintf("%s %s", time.Now().Format("15:04:05"), line)

		logMu.Lock()
		logLines = append(logLines, entry)
		if len(logLines) > 500 {
			logLines = logLines[len(logLines)-500:]
		}
		text := strings.Join(logLines, "\n")
		logMu.Unlock()

		fyne.Do(func() {
			logView.SetText(text)
		})
	}

	controller := newRuntimeController(appendLog)

	sourceEntry := widget.NewEntry()
	sourceEntry.SetPlaceHolder("请选择源目录（绝对路径）")
	targetEntry := widget.NewEntry()
	targetEntry.SetPlaceHolder("请选择目标目录（绝对路径）")
	startupCheck := widget.NewCheck("开机自启动", nil)

	if len(cfg.Jobs) > 0 {
		sourceEntry.SetText(cfg.Jobs[0].SourceDir)
		targetEntry.SetText(cfg.Jobs[0].TargetDir)
	}
	startupCheck.SetChecked(cfg.App.Startup.Enabled)

	statusValue := widget.NewLabel("未运行")
	summaryValue := widget.NewLabel("暂无运行统计")
	logPathValue := widget.NewLabel("日志文件：未生成")

	setStatus := func(text string) {
		fyne.Do(func() {
			statusValue.SetText(text)
		})
	}
	setSummary := func(text string) {
		fyne.Do(func() {
			summaryValue.SetText(text)
		})
	}
	setLogPath := func(path string) {
		if strings.TrimSpace(path) == "" {
			fyne.Do(func() {
				logPathValue.SetText("日志文件：未生成")
			})
			return
		}
		fyne.Do(func() {
			logPathValue.SetText("日志文件：" + path)
		})
	}

	saveConfig := func() (api.Config, error) {
		latest, err := cfgSvc.Load(context.Background())
		if err != nil {
			return api.Config{}, err
		}

		next, err := buildConfigFromUI(latest, sourceEntry.Text, targetEntry.Text, startupCheck.Checked)
		if err != nil {
			return api.Config{}, err
		}

		if err := cfgSvc.Save(context.Background(), next); err != nil {
			return api.Config{}, err
		}

		appendLog("配置已保存：" + configPath)
		return next, nil
	}

	chooseSourceBtn := widget.NewButton("选择源目录", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, win)
				return
			}
			if uri == nil {
				return
			}
			sourceEntry.SetText(uri.Path())
		}, win)
	})

	chooseTargetBtn := widget.NewButton("选择目标目录", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, win)
				return
			}
			if uri == nil {
				return
			}
			targetEntry.SetText(uri.Path())
		}, win)
	})

	saveBtn := widget.NewButton("保存配置", func() {
		if _, err := saveConfig(); err != nil {
			appendLog("保存配置失败: " + err.Error())
			dialog.ShowError(err, win)
			return
		}
	})

	startBtn := widget.NewButton("启动同步", nil)
	stopBtn := widget.NewButton("停止同步", nil)
	syncBtn := widget.NewButton("立即同步", nil)
	reportBtn := widget.NewButton("导出报告", nil)
	openLogBtn := widget.NewButton("打开日志目录", nil)
	refreshStatusBtn := widget.NewButton("刷新状态", nil)

	applyButtonState := func() {
		running := controller.Running()
		if running {
			startBtn.Disable()
			stopBtn.Enable()
			syncBtn.Enable()
			reportBtn.Enable()
			openLogBtn.Enable()
			refreshStatusBtn.Enable()
			return
		}
		startBtn.Enable()
		stopBtn.Disable()
		syncBtn.Disable()
		reportBtn.Disable()
		openLogBtn.Disable()
		refreshStatusBtn.Disable()
	}

	refreshStatus := func() {
		summary, ok := controller.Summary()
		if !ok {
			setSummary("暂无运行统计")
			setLogPath(controller.LogFile())
			return
		}
		setSummary(fmt.Sprintf(
			"任务=%d | copied=%d updated=%d deleted=%d skipped=%d conflicts=%d errors=%d",
			summary.JobCount,
			summary.Totals.CopiedFiles,
			summary.Totals.UpdatedFiles,
			summary.Totals.DeletedFiles,
			summary.Totals.SkippedFiles,
			summary.Totals.ConflictCount,
			summary.Totals.ErrorCount,
		))
		setLogPath(controller.LogFile())
	}

	startBtn.OnTapped = func() {
		go func() {
			nextCfg, err := saveConfig()
			if err != nil {
				appendLog("启动失败: " + err.Error())
				fyne.Do(func() {
					dialog.ShowError(err, win)
				})
				return
			}

			if err := controller.Start(nextCfg, configPath); err != nil {
				appendLog("启动失败: " + err.Error())
				fyne.Do(func() {
					dialog.ShowError(err, win)
				})
				return
			}

			appendLog("同步服务已启动")
			setStatus("运行中")
			refreshStatus()
			fyne.Do(func() {
				applyButtonState()
			})
		}()
	}

	stopBtn.OnTapped = func() {
		go func() {
			if err := controller.Stop(); err != nil {
				appendLog("停止失败: " + err.Error())
				fyne.Do(func() {
					dialog.ShowError(err, win)
				})
				return
			}
			appendLog("同步服务已停止")
			setStatus("未运行")
			refreshStatus()
			fyne.Do(func() {
				applyButtonState()
			})
		}()
	}

	syncBtn.OnTapped = func() {
		go func() {
			if err := controller.SyncNowAll(); err != nil {
				appendLog("立即同步失败: " + err.Error())
				fyne.Do(func() {
					dialog.ShowError(err, win)
				})
				return
			}
			appendLog("已触发全部任务立即同步")
			refreshStatus()
		}()
	}

	reportBtn.OnTapped = func() {
		go func() {
			path, err := controller.ExportReport()
			if err != nil {
				appendLog("导出报告失败: " + err.Error())
				fyne.Do(func() {
					dialog.ShowError(err, win)
				})
				return
			}
			appendLog("报告已导出: " + path)
		}()
	}

	openLogBtn.OnTapped = func() {
		path := controller.LogFile()
		if strings.TrimSpace(path) == "" {
			appendLog("日志文件尚未生成")
			return
		}
		dir := filepath.Dir(path)
		if err := openInFileManager(dir); err != nil {
			appendLog("打开日志目录失败: " + err.Error())
			dialog.ShowError(err, win)
			return
		}
		appendLog("已打开日志目录: " + dir)
	}

	refreshStatusBtn.OnTapped = func() {
		refreshStatus()
		appendLog("状态已刷新")
	}

	applyButtonState()

	configCard := widget.NewCard("任务配置", "当前 GUI 默认编辑第 1 个任务（jobs[0]）", container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("配置文件", widget.NewLabel(configPath)),
			widget.NewFormItem("源目录", container.NewBorder(nil, nil, nil, chooseSourceBtn, sourceEntry)),
			widget.NewFormItem("目标目录", container.NewBorder(nil, nil, nil, chooseTargetBtn, targetEntry)),
		),
		startupCheck,
		saveBtn,
	))

	statusCard := widget.NewCard("运行状态", "GUI 只作为控制面板，核心同步逻辑沿用现有 runtime", container.NewVBox(
		widget.NewForm(
			widget.NewFormItem("服务状态", statusValue),
			widget.NewFormItem("统计摘要", summaryValue),
			widget.NewFormItem("日志路径", logPathValue),
		),
		container.NewGridWithColumns(3,
			startBtn,
			stopBtn,
			syncBtn,
			reportBtn,
			openLogBtn,
			refreshStatusBtn,
		),
	))

	logsCard := widget.NewCard("运行日志", "显示 GUI 操作与关键状态（详细日志请查看文件）", logView)

	content := container.NewBorder(
		container.NewVBox(configCard, statusCard),
		nil,
		nil,
		nil,
		container.NewPadded(logsCard),
	)
	win.SetContent(content)

	setStatus("未运行")
	refreshStatus()
	appendLog("GUI 已启动，先配置源目录与目标目录，然后点击“启动同步”")
	appendLog("CLI 后台模式仍可使用：go run ./cmd/litesync")

	closing := false
	win.SetCloseIntercept(func() {
		if closing {
			return
		}
		closing = true
		appendLog("正在安全退出...")
		go func() {
			_ = controller.Stop()
			fyne.Do(func() {
				win.SetCloseIntercept(nil)
				win.Close()
			})
		}()
	})

	win.ShowAndRun()
}

type runtimeController struct {
	opMu sync.Mutex
	mu   sync.RWMutex

	running bool

	ctx    context.Context
	cancel context.CancelFunc

	logger      *logx.SLogger
	logFile     string
	backup      *backup.Manager
	scheduler   *scheduler.Dispatcher
	watcher     *watcher.Service
	startup     *startup.Service
	stateStore  *state.FileStore
	reporter    *state.ReportExporter
	activeJobs  []api.JobID
	eventWg     sync.WaitGroup
	appendLogFn func(string)
}

func newRuntimeController(appendLogFn func(string)) *runtimeController {
	return &runtimeController{appendLogFn: appendLogFn}
}

func (r *runtimeController) Running() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

func (r *runtimeController) Start(cfg api.Config, configPath string) error {
	r.opMu.Lock()
	defer r.opMu.Unlock()

	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return api.Wrap(api.ErrAlreadyRunning, "runtime already running")
	}
	r.mu.Unlock()

	logDir := strings.TrimSpace(cfg.App.LogDir)
	if logDir == "" {
		defaultLogDir, err := config.DefaultLogDir()
		if err != nil {
			return err
		}
		logDir = defaultLogDir
	}
	stateDir := strings.TrimSpace(cfg.App.StateDir)
	if stateDir == "" {
		defaultStateDir, err := config.DefaultStateDir()
		if err != nil {
			return err
		}
		stateDir = defaultStateDir
	}

	baseLogger, logFile, err := logx.NewWithFile(cfg.App.LogLevel, logDir)
	if err != nil {
		return err
	}
	logger := newUILogger(baseLogger, r.appendLogFn)

	backupManager := backup.New(logger)
	backupManager.ReplaceJobs(cfg.Jobs)
	schedulerSvc := scheduler.New(backupManager, logger)
	schedulerSvc.ConfigureJobs(cfg.Jobs)
	schedulerSvc.EnableRecovery(state.NewPendingEventStore(stateDir))
	watcherSvc := watcher.New()
	startupSvc := startup.New()
	stateStore := state.NewFileStore(stateDir)
	reporter := state.NewReportExporter(stateDir)

	runCtx, cancel := context.WithCancel(context.Background())
	if err := schedulerSvc.Start(runCtx); err != nil {
		_ = baseLogger.Sync()
		cancel()
		return err
	}

	startupStatus, err := startupSvc.Status(runCtx)
	if err != nil {
		logger.Warn("read startup status failed", api.Field{Key: "error", Value: err.Error()})
	}
	if cfg.App.Startup.Enabled && !startupStatus.Enabled {
		if err := startupSvc.Enable(runCtx); err != nil {
			logger.Warn("enable startup failed", api.Field{Key: "error", Value: err.Error()})
		}
	}
	if !cfg.App.Startup.Enabled && startupStatus.Enabled {
		if err := startupSvc.Disable(runCtx); err != nil {
			logger.Warn("disable startup failed", api.Field{Key: "error", Value: err.Error()})
		}
	}
	startupStatus, err = startupSvc.Status(runCtx)
	if err != nil {
		logger.Warn("refresh startup status failed", api.Field{Key: "error", Value: err.Error()})
	}

	activeJobs := make([]api.JobID, 0, len(cfg.Jobs))
	for _, job := range cfg.Jobs {
		if !job.Enabled {
			logger.Info("skip disabled job", api.Field{Key: "job_id", Value: job.ID})
			continue
		}

		if err := schedulerSvc.RegisterJob(runCtx, job.ID); err != nil {
			logger.Error("register job failed", err, api.Field{Key: "job_id", Value: job.ID})
			continue
		}

		activeJobs = append(activeJobs, job.ID)

		if err := watcherSvc.Start(runCtx, job.ID, job.SourceDir); err != nil {
			logger.Error("start watcher failed", err, api.Field{Key: "job_id", Value: job.ID})
		}

		if isInitialFullSync(job) {
			res, syncErr := backupManager.SyncNow(runCtx, api.SyncRequest{
				JobID:       job.ID,
				RequestID:   api.RequestID(fmt.Sprintf("startup-%d", time.Now().UnixNano())),
				Reason:      api.TriggerStartup,
				Mode:        api.SyncModeFull,
				RequestedAt: time.Now(),
			})
			if syncErr != nil {
				logger.Error(
					"startup full sync failed",
					syncErr,
					api.Field{Key: "job_id", Value: job.ID},
					api.Field{Key: "run_id", Value: res.RunID},
				)
			} else {
				logger.Info(
					"startup full sync success",
					api.Field{Key: "job_id", Value: job.ID},
					api.Field{Key: "run_id", Value: res.RunID},
					api.Field{Key: "copied", Value: res.CopiedFiles},
					api.Field{Key: "updated", Value: res.UpdatedFiles},
					api.Field{Key: "skipped", Value: res.SkippedFiles},
				)
			}
		}
	}

	if len(activeJobs) == 0 {
		cancel()
		_ = schedulerSvc.Stop(context.Background())
		_ = baseLogger.Sync()
		return api.Wrap(api.ErrInvalidArgument, "no enabled jobs, please configure jobs[0] first")
	}

	r.eventWg.Add(1)
	go func() {
		defer r.eventWg.Done()
		for {
			select {
			case <-runCtx.Done():
				return
			case event := <-watcherSvc.Events():
				if err := schedulerSvc.PushEvent(runCtx, event); err != nil {
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

	logger.Info(
		"LiteSync GUI runtime started",
		api.Field{Key: "config_path", Value: configPath},
		api.Field{Key: "jobs", Value: len(cfg.Jobs)},
		api.Field{Key: "active_jobs", Value: len(activeJobs)},
		api.Field{Key: "startup_provider", Value: startupStatus.Provider},
		api.Field{Key: "startup_enabled", Value: startupStatus.Enabled},
		api.Field{Key: "watcher_impl", Value: fmt.Sprintf("%T", watcherSvc)},
	)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.running = true
	r.ctx = runCtx
	r.cancel = cancel
	r.logger = baseLogger
	r.logFile = logFile
	r.backup = backupManager
	r.scheduler = schedulerSvc
	r.watcher = watcherSvc
	r.startup = startupSvc
	r.stateStore = stateStore
	r.reporter = reporter
	r.activeJobs = activeJobs
	return nil
}

func (r *runtimeController) Stop() error {
	r.opMu.Lock()
	defer r.opMu.Unlock()

	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return nil
	}

	cancel := r.cancel
	watcherSvc := r.watcher
	schedulerSvc := r.scheduler
	backupManager := r.backup
	stateStore := r.stateStore
	logger := r.logger
	activeJobs := append([]api.JobID(nil), r.activeJobs...)

	r.running = false
	r.ctx = nil
	r.cancel = nil
	r.watcher = nil
	r.scheduler = nil
	r.backup = nil
	r.stateStore = nil
	r.reporter = nil
	r.activeJobs = nil
	r.mu.Unlock()

	var errs []error
	if cancel != nil {
		cancel()
	}

	shutdownCtx, timeoutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer timeoutCancel()

	for _, jobID := range activeJobs {
		if watcherSvc == nil {
			break
		}
		if err := watcherSvc.Stop(shutdownCtx, jobID); err != nil {
			errs = append(errs, err)
		}
	}
	if schedulerSvc != nil {
		if err := schedulerSvc.Stop(shutdownCtx); err != nil {
			errs = append(errs, err)
		}
	}

	r.eventWg.Wait()

	if backupManager != nil && stateStore != nil {
		snapshot := backupManager.RuntimeSnapshot()
		if err := stateStore.Save(snapshot); err != nil {
			errs = append(errs, err)
		}
	}
	if logger != nil {
		if err := logger.Sync(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (r *runtimeController) SyncNowAll() error {
	r.mu.RLock()
	if !r.running || r.scheduler == nil {
		r.mu.RUnlock()
		return api.Wrap(api.ErrInternal, "runtime is not running")
	}
	schedulerSvc := r.scheduler
	jobs := append([]api.JobID(nil), r.activeJobs...)
	r.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	var errs []error
	for _, jobID := range jobs {
		runID, err := schedulerSvc.TriggerNow(ctx, jobID, api.TriggerManual)
		if err != nil {
			errs = append(errs, fmt.Errorf("job=%s: %w", jobID, err))
			continue
		}
		r.appendLogFn(fmt.Sprintf("manual sync queued: job=%s run_id=%s", jobID, runID))
	}
	return errors.Join(errs...)
}

func (r *runtimeController) ExportReport() (string, error) {
	r.mu.RLock()
	if !r.running || r.reporter == nil || r.backup == nil {
		r.mu.RUnlock()
		return "", api.Wrap(api.ErrInternal, "runtime is not running")
	}
	reporter := r.reporter
	backupManager := r.backup
	r.mu.RUnlock()

	summary := backupManager.RuntimeSummary()
	snapshot := backupManager.RuntimeSnapshot()
	return reporter.Export(summary, snapshot)
}

func (r *runtimeController) Summary() (api.RuntimeSummary, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.running || r.backup == nil {
		return api.RuntimeSummary{}, false
	}
	return r.backup.RuntimeSummary(), true
}

func (r *runtimeController) LogFile() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.logFile
}

type uiLogger struct {
	base api.Logger
	on   func(string)
}

func newUILogger(base api.Logger, on func(string)) *uiLogger {
	return &uiLogger{base: base, on: on}
}

func (l *uiLogger) Debug(msg string, fields ...api.Field) {
	l.base.Debug(msg, fields...)
	l.emit("DEBUG", msg, nil, fields)
}

func (l *uiLogger) Info(msg string, fields ...api.Field) {
	l.base.Info(msg, fields...)
	l.emit("INFO", msg, nil, fields)
}

func (l *uiLogger) Warn(msg string, fields ...api.Field) {
	l.base.Warn(msg, fields...)
	l.emit("WARN", msg, nil, fields)
}

func (l *uiLogger) Error(msg string, err error, fields ...api.Field) {
	l.base.Error(msg, err, fields...)
	l.emit("ERROR", msg, err, fields)
}

func (l *uiLogger) With(fields ...api.Field) api.Logger {
	return &uiLogger{
		base: l.base.With(fields...),
		on:   l.on,
	}
}

func (l *uiLogger) Sync() error {
	return l.base.Sync()
}

func (l *uiLogger) emit(level string, msg string, err error, fields []api.Field) {
	if l.on == nil {
		return
	}
	parts := []string{level, msg}
	if err != nil {
		parts = append(parts, "error="+err.Error())
	}
	for _, field := range fields {
		parts = append(parts, fmt.Sprintf("%s=%v", field.Key, field.Value))
	}
	l.on(strings.Join(parts, " | "))
}

func ensureConfig(ctx context.Context, svc *config.FileService) (api.Config, error) {
	cfg, err := svc.Load(ctx)
	if err != nil {
		return api.Config{}, err
	}
	if _, err := os.Stat(svc.Path()); errors.Is(err, os.ErrNotExist) {
		if saveErr := svc.Save(ctx, cfg); saveErr != nil {
			return api.Config{}, saveErr
		}
	}
	return cfg, nil
}

func buildConfigFromUI(existing api.Config, sourceDir string, targetDir string, startupEnabled bool) (api.Config, error) {
	sourceDir = strings.TrimSpace(sourceDir)
	targetDir = strings.TrimSpace(targetDir)
	if sourceDir == "" || targetDir == "" {
		return api.Config{}, api.Wrap(api.ErrInvalidArgument, "source_dir and target_dir are required")
	}
	if !filepath.IsAbs(sourceDir) || !filepath.IsAbs(targetDir) {
		return api.Config{}, api.Wrap(api.ErrInvalidArgument, "source_dir and target_dir must be absolute paths")
	}

	next := existing
	next.ApplyDefaults()
	next.App.Startup.Enabled = startupEnabled
	next.App.RunMode = "window"

	if len(next.Jobs) == 0 {
		next.Jobs = []api.Job{{
			ID:        api.JobID("job-default"),
			Enabled:   true,
			SourceDir: filepath.Clean(sourceDir),
			TargetDir: filepath.Clean(targetDir),
			Exclude:   []string{},
			Strategy: api.Strategy{
				Mode:        api.DefaultJobMode,
				InitialSync: api.DefaultInitialSync,
				EventSync: api.EventSync{
					DebounceMS: api.DefaultEventDebounceMS,
				},
				PeriodicReconcile: api.PeriodicReconcile{
					Enabled:         api.DefaultPeriodicReconcileEnabled,
					IntervalMinutes: api.DefaultPeriodicIntervalMinutes,
				},
				DeletePolicy:      api.DefaultDeletePolicy,
				ConflictPolicy:    api.DefaultConflictPolicy,
				MaxParallelCopies: api.DefaultMaxParallelCopies,
			},
		}}
	} else {
		job := next.Jobs[0]
		job.Enabled = true
		job.SourceDir = filepath.Clean(sourceDir)
		job.TargetDir = filepath.Clean(targetDir)
		next.Jobs[0] = job
	}

	next.ApplyDefaults()
	return next, config.NewFileService("-").Validate(next)
}

func openInFileManager(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return api.Wrap(api.ErrInvalidArgument, "path is empty")
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

func isInitialFullSync(job api.Job) bool {
	return strings.EqualFold(strings.TrimSpace(job.Strategy.InitialSync), "full")
}

func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
	os.Exit(1)
}
