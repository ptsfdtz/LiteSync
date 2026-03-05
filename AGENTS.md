# LiteSync Agent 启动文档

> 目的：让任意设备上的 AI Agent 在进入仓库后，能在 10 分钟内理解项目并开始开发。  
> 范围：本文件面向 AI 开发助手，不面向最终用户。

## 1. 项目快照（Machine-Readable）

```yaml
project:
  name: LiteSync
  type: cross_platform_desktop_backup
  status: bootstrap_initialized
  sync_direction: source_to_target

stack:
  language: Go
  gui: Fyne
  config_format: YAML
  target_platforms: [Windows, macOS, Linux]

goals:
  - select_source_directory
  - select_target_directory
  - auto_sync_changes
  - startup_and_background_run
  - low_disturbance_and_recoverable

non_goals_current:
  - two_way_sync
  - cloud_sync
  - distributed_realtime_consistency

default_policies:
  conflict_policy: backup_then_overwrite
  delete_policy: propagate
  reconcile_enabled: true

implemented_foundation:
  - go_module_initialized
  - project_structure_initialized
  - config_file_service_bootstrap
  - module_stubs_created
  - initial_full_sync_bootstrap
  - file_event_watcher_bootstrap
  - scheduler_debounce_and_serial_execution_bootstrap
  - incremental_sync_pipeline_bootstrap
  - logging_persistence_and_error_codes_bootstrap
  - startup_service_cross_platform_bootstrap
  - periodic_reconcile_bootstrap
  - graceful_shutdown_and_runtime_state_bootstrap
  - background_command_control_bootstrap
  - multi_job_isolation_bootstrap
  - conflict_policy_handling_bootstrap
```

## 2. Agent 首次启动流程

1. 先读本文件，再读 `README.md`。
2. 按顺序读取：
   - `docs/ARCHITECTURE.md`
   - `docs/API.md`
   - `docs/CONFIG.md`
   - `docs/TODO.md`
   - `docs/ROADMAP.md`
   - `docs/FAQ.md`
3. 仅从 `P0` 开始开发，不跨阶段提前实现 `P1/P2`。
4. 任何实现前，先核对接口命名是否与 `docs/API.md` 一致。
5. 任何行为变更，必须同步更新对应文档。

## 3. 统一术语（必须一致）

| 术语 | 含义 |
| --- | --- |
| `job_id` | 备份任务唯一标识 |
| `run_id` | 一次同步执行实例标识 |
| `full sync` | 首次全量同步 |
| `incremental sync` | 事件驱动增量同步 |
| `reconcile` | 周期校验与补偿同步 |
| `debounce_ms` | 事件防抖时间窗口 |
| `conflict_policy` | 冲突处理策略 |
| `delete_policy` | 删除传播策略 |

## 4. 实现优先级（严格执行）

按 `docs/TODO.md` 的 `P0-01` 到 `P0-10` 顺序推进。建议最小可交付切片如下：

1. `P0-01` 配置加载与校验
2. `P0-02` 首次全量同步
3. `P0-03` 监听事件标准化
4. `P0-04` 调度防抖与串行化
5. `P0-05` 增量同步

达到上述 5 项后，再接入自启动、后台运行、周期校验和安全退出。

## 5. 代码目录目标（规划中）

```text
cmd/litesync/
internal/backup/
internal/watcher/
internal/scheduler/
internal/config/
internal/startup/
internal/logx/
```

Agent 在初始化代码时应优先遵循该结构，除非有充分理由并同步更新文档。

## 6. 设计约束

- 同一 `job_id` 任一时刻仅允许一个活动同步流程。
- 监听事件不保证完整，必须由 `reconcile` 补偿。
- 删除/冲突行为必须显式受策略字段控制。
- 不得实现双向同步逻辑。
- 错误必须带可归类语义（便于日志和 UI 呈现）。

## 7. 跨平台最小检查清单

在任意设备开始开发前，先完成以下检查：

1. `go version` 满足 `1.22+`（建议）。
2. 能运行基础命令：`go env`、`go test ./...`（即使初期无测试）。
3. 确认本机目标平台（Windows/macOS/Linux）与路径语义。
4. 如实现自启动功能，先确认本平台机制（注册表/LaunchAgents/autostart）。

## 8. 文档一致性规则

- 改接口：同步 `docs/API.md` 与 `docs/ARCHITECTURE.md`。
- 改配置：同步 `docs/CONFIG.md` 与 `README.md`。
- 改里程碑或任务优先级：同步 `docs/TODO.md` 与 `docs/ROADMAP.md`。
- 文档内“规划中”标记不能丢失，直到功能真正落地。

## 9. Agent 工作输出要求

- 每个实现任务至少包含：
  - 变更摘要
  - 受影响模块
  - 风险点
  - 对应 TODO 条目
- 若无法完成任务，必须说明阻塞原因与下一步建议。

## 10. 开发入口结论

- 若你是新启动的 Agent：从 `P0-01` 开始，不要跳过配置与错误语义设计。
- 若你是继续开发的 Agent：先读取最新 `TODO` 勾选状态，再选择下一个未完成 P0 任务。
