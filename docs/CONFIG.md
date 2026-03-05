# LiteSync 配置规范（规划中）

> 本文定义 LiteSync 的配置文件结构、字段语义、默认值和热更新策略。  
> 当前阶段选择 `YAML` 作为配置格式。

## 1. 配置格式与位置

- 格式：`YAML`
- 文件名：`config.yaml`
- 建议路径（规划中）：
  - Windows: `%APPDATA%\\LiteSync\\config.yaml`
  - macOS: `~/Library/Application Support/LiteSync/config.yaml`
  - Linux: `~/.config/litesync/config.yaml`

## 2. 顶层结构

```yaml
version: 1
app:
  language: zh-CN
  run_mode: tray
  log_level: info
  log_dir: ""
  state_dir: ""
  startup:
    enabled: true
jobs: []
```

## 3. 字段定义

## 3.1 顶层与 app 字段

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `version` | `int` | 是 | `1` | 配置版本号，用于后续迁移 |
| `app.language` | `string` | 否 | `zh-CN` | UI 语言（规划中） |
| `app.run_mode` | `string` | 否 | `tray` | 启动模式：`tray/window` |
| `app.log_level` | `string` | 否 | `info` | 日志级别：`debug/info/warn/error` |
| `app.log_dir` | `string` | 否 | 平台默认目录 | 日志目录，空字符串表示使用默认 |
| `app.state_dir` | `string` | 否 | 平台默认目录 | 状态文件目录，空字符串表示使用默认 |
| `app.startup.enabled` | `bool` | 否 | `true` | 是否启用开机自启动 |

## 3.2 `jobs[]` 字段（备份任务）

| 字段 | 类型 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| `jobs[].id` | `string` | 是 | 无 | 任务唯一标识 |
| `jobs[].enabled` | `bool` | 否 | `true` | 任务是否启用 |
| `jobs[].source_dir` | `string` | 是 | 无 | 源目录绝对路径 |
| `jobs[].target_dir` | `string` | 是 | 无 | 目标目录绝对路径 |
| `jobs[].exclude` | `[]string` | 否 | `[]` | 排除规则，支持 glob（规划中） |
| `jobs[].strategy.mode` | `string` | 否 | `mirror` | 同步模式，当前仅支持 `mirror` |
| `jobs[].strategy.initial_sync` | `string` | 否 | `full` | 首次同步策略，当前建议 `full` |
| `jobs[].strategy.event_sync.debounce_ms` | `int` | 否 | `1500` | 事件聚合防抖时间（毫秒） |
| `jobs[].strategy.periodic_reconcile.enabled` | `bool` | 否 | `true` | 是否开启周期校验 |
| `jobs[].strategy.periodic_reconcile.interval_minutes` | `int` | 否 | `30` | 周期校验间隔（分钟） |
| `jobs[].strategy.delete_policy` | `string` | 否 | `propagate` | 删除策略：`propagate/soft_delete/ignore` |
| `jobs[].strategy.conflict_policy` | `string` | 否 | `backup_then_overwrite` | 冲突策略：`overwrite/backup_then_overwrite/skip` |
| `jobs[].strategy.max_parallel_copies` | `int` | 否 | `4` | 全量同步并行复制 worker 数 |
| `jobs[].strategy.follow_symlinks` | `bool` | 否 | `false` | 是否跟随软链接 |
| `jobs[].strategy.preserve_permissions` | `bool` | 否 | `true` | 是否保留文件权限（尽力而为） |

## 4. 默认值规则

- `log_dir/state_dir` 为空时由程序按平台选择默认目录。
- `source_dir` 和 `target_dir` 必须是绝对路径，且不得相同。
- 未设置 `jobs` 时，程序应提示“无可运行任务”并保持空闲。

## 5. 示例配置

```yaml
version: 1
app:
  language: zh-CN
  run_mode: tray
  log_level: info
  log_dir: ""
  state_dir: ""
  startup:
    enabled: true

jobs:
  - id: job-docs
    enabled: true
    source_dir: "D:/Work/docs"
    target_dir: "E:/Backup/docs"
    exclude:
      - ".git/**"
      - "*.tmp"
      - "~$*"
    strategy:
      mode: mirror
      initial_sync: full
      event_sync:
        debounce_ms: 1500
      periodic_reconcile:
        enabled: true
        interval_minutes: 30
      delete_policy: propagate
      conflict_policy: backup_then_overwrite
      max_parallel_copies: 4
      follow_symlinks: false
      preserve_permissions: true
```

## 6. 配置热更新策略

> 说明：热更新能力为**规划中**。若热更新失败，回退到“保留旧配置并记录错误”。

| 字段 | 是否支持热更新 | 生效方式 |
| --- | --- | --- |
| `app.log_level` | 是 | 立即生效 |
| `app.run_mode` | 否 | 下次启动生效 |
| `app.startup.enabled` | 是 | 触发 `StartupService` 即时应用 |
| `jobs[].exclude` | 是 | 重新加载任务监听后生效 |
| `jobs[].strategy.event_sync.debounce_ms` | 是 | 下一个调度窗口生效 |
| `jobs[].source_dir` | 否 | 需重建监听并重启任务 |
| `jobs[].target_dir` | 否 | 需重启任务后生效 |
| `jobs[].strategy.conflict_policy` | 是 | 下一次同步生效 |
| `jobs[].strategy.delete_policy` | 是 | 下一次同步生效 |

## 7. 配置校验规则

- 路径规则：
  - `source_dir` 与 `target_dir` 不能为空
  - 二者必须不同
  - 必须是可访问路径（无权限时报错）
- 任务规则：
  - `jobs[].id` 全局唯一
  - 禁止多个任务写入同一目标目录（规划中可放宽）
- 策略规则：
  - `interval_minutes` 最小值建议 `5`
  - `debounce_ms` 建议区间 `200 ~ 10000`

## 8. 版本迁移策略（规划中）

- 迁移原则：
  - 向后兼容优先
  - 不可识别字段保留但忽略
  - 迁移失败时不覆盖原文件
- 迁移流程：
  1. 读取旧版本配置
  2. 生成新版本结构
  3. 写入临时文件并校验
  4. 原子替换
