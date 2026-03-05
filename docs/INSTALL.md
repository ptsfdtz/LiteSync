# LiteSync 安装与升级指南（P2）

## 1. 平台安装步骤

## 1.1 Windows

1. 准备可执行文件 `litesync.exe`（来自 `go build` 或发布包）。
2. 放置到固定目录，例如：`C:\Program Files\LiteSync\litesync.exe`。
3. 运行一次初始化配置：
   - `litesync.exe -init-config`
4. 编辑配置文件：
   - `%APPDATA%\LiteSync\config.yaml`
5. 启动：
   - `litesync.exe`

## 1.2 macOS

1. 准备可执行文件 `litesync`。
2. 放置到固定目录，例如：`/Applications/LiteSync/litesync`。
3. 初始化配置：`./litesync -init-config`
4. 编辑配置文件：
   - `~/Library/Application Support/LiteSync/config.yaml`
5. 启动：`./litesync`

## 1.3 Linux

1. 准备可执行文件 `litesync`。
2. 放置到固定目录，例如：`/opt/litesync/litesync`。
3. 初始化配置：`./litesync -init-config`
4. 编辑配置文件：
   - `~/.config/litesync/config.yaml`
5. 启动：`./litesync`

## 2. 升级流程

1. 记录当前版本与路径（`litesync --version`，规划中）。
2. 安全退出正在运行的 LiteSync（`exit` 命令或 Ctrl+C）。
3. 备份以下目录：
   - 配置目录（`config.yaml`）
   - 状态目录（`state/`）
   - 日志目录（`logs/`）
4. 替换可执行文件为新版本。
5. 启动新版本并检查：
   - 日志是否有启动错误
   - `status` 输出是否正常

## 3. 回滚步骤

1. 停止当前版本。
2. 替换回上一版本可执行文件。
3. 若发生配置不兼容：
   - 恢复备份的 `config.yaml`
   - 恢复备份的 `state/` 目录
4. 重启并验证同步任务。

## 4. 数据保留策略

- 升级/回滚均应保留：
  - `config.yaml`
  - `state/runtime_state.json`
  - `state/pending_events.json`
  - `logs/*.log`
- 避免在升级时删除 `state/`，否则会丢失恢复上下文。
