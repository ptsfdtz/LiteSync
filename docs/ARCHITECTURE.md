# LiteSync 架构说明（MVP）

## 总体架构

- 前端（`client/apps/web`）
  - React + Vite
  - 使用 `@workspace/ui`（`shadcn/ui` 风格组件）构建界面
  - 通过 HTTP 调用后端 API
- 后端（`server`）
  - Go 单进程本地服务
  - 负责配置管理、调度、文件监听、备份执行、状态和日志输出

## 后端分层

- `internal/httpapi`
  - 对外 REST API 层
  - 参数解析、错误码、JSON 序列化、CORS
- `internal/service`
  - 核心业务编排
  - 配置更新、调度器启停、监听器启停、备份状态管理
- `internal/backup`
  - 备份执行器
  - 将源目录复制到目标目录下时间戳快照目录
- `internal/watcher`
  - 基于 fsnotify 的目录递归监听
  - 防抖后触发备份，避免高频抖动
- `internal/config`
  - 配置持久化到本地 JSON 文件
- `internal/logs`
  - 内存环形日志缓冲（供页面展示）

## 运行模型

1. 服务启动时读取配置文件
1. 根据配置启动：
   - 定时调度器（`intervalMinutes`）
   - 文件监听器（`watchChanges`）
1. 触发条件（手动 / 定时 / 文件变化）进入统一备份流程
1. 备份完成后更新状态统计并写入日志

## 数据持久化

- 当前持久化内容：`config.json`
- 默认路径：
  - 本地运行：用户配置目录下 `LiteSync/`
  - Docker：`/data`（映射到宿主机 `./data`）

## 跨平台策略

- 后端基于 Go 标准库 + fsnotify，支持 Windows/Linux/macOS
- 前端纯 Web，不依赖平台特定 UI 框架
- Docker 提供统一部署方式

## 非目标（MVP）

- 云备份
- 多设备同步
- 团队协作
- 复杂权限系统
- 双向同步
- 移动端

