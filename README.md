# LiteSync

LiteSync 是一个跨平台的本地自动备份服务（MVP），采用前后端分离架构：

- 后端：Go，负责备份执行、调度、文件监听、日志、配置持久化
- 前端：React + `shadcn/ui`，提供 Web 管理界面
- 运行方式：支持本地二进制运行与 Docker 运行
- 目标平台：Windows / Linux / macOS

## 目录结构

- `client/` Web 前端（全部基于 `@workspace/ui` 组件）
- `server/` Go 本地服务和 API
- `docs/` 架构与接口文档

## MVP 功能覆盖

- 启动后可通过浏览器访问管理界面
- 配置源目录 / 目标目录 / 备份频率 / 文件变化自动触发
- 配置持久化保存（重启后仍生效）
- 支持手动触发备份 + 定时备份 + 文件变化触发备份
- 展示任务状态、最近日志和错误信息
- 备份结果写入目标目录的时间戳快照目录

## 本地运行（推荐开发与验证）

### 1) 启动后端 API（Go）

```powershell
cd server
go run ./cmd/litesync-server
```

默认后端地址：`http://localhost:8080`

### 2) 启动前端（开发模式）

```powershell
cd client
pnpm install
pnpm --filter web dev
```

默认前端地址：`http://localhost:5173`  
开发模式下已配置 `/api` 代理到 `http://localhost:8080`。

### 3) 单服务方式（后端直接托管前端静态页面）

```powershell
cd client
pnpm --filter web build

cd ../server
$env:LITESYNC_WEB_DIR="../client/apps/web/dist"
go run ./cmd/litesync-server
```

然后访问：`http://localhost:8080`

## Docker 运行

在仓库根目录执行：

```powershell
docker compose up --build
```

访问地址：

- 前端界面：`http://localhost:5173`
- 后端 API：`http://localhost:8080/api`

配置数据默认持久化在根目录 `./data`（由 `docker-compose.yml` 挂载）。

## 关键环境变量

- `LITESYNC_HTTP_ADDR`：后端监听地址，默认 `:8080`
- `LITESYNC_DATA_DIR`：配置数据目录，默认用户配置目录下 `LiteSync`
- `LITESYNC_WEB_DIR`：后端托管前端静态文件目录，默认 `./web`
- `VITE_API_BASE_URL`：前端 API 基地址（构建时），默认 `/api`

## 接口概览

- `GET /api/health` 健康检查
- `GET /api/config` 获取配置
- `PUT /api/config` 保存配置
- `GET /api/status` 获取运行状态
- `GET /api/logs?limit=120` 获取最近日志
- `POST /api/backup` 手动触发备份

详细说明见 [docs/API.md](./docs/API.md)。
