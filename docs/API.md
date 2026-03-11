# LiteSync API（MVP）

Base URL: `/api`

## 1. 健康检查

### `GET /health`

响应示例：

```json
{
  "status": "ok",
  "time": "2026-03-11T06:10:00Z"
}
```

## 2. 获取配置

### `GET /config`

响应示例：

```json
{
  "sourceDir": "C:\\Users\\you\\Documents",
  "targetDir": "D:\\Backups\\LiteSync",
  "intervalMinutes": 60,
  "watchChanges": true
}
```

## 3. 保存配置

### `PUT /config`

请求体：

```json
{
  "sourceDir": "C:\\Users\\you\\Documents",
  "targetDir": "D:\\Backups\\LiteSync",
  "intervalMinutes": 30,
  "watchChanges": true
}
```

响应：返回保存后的配置对象。

常见校验错误：

- `sourceDir is required`
- `targetDir is required`
- `intervalMinutes must be between 1 and 10080`
- `targetDir cannot be inside sourceDir`

## 4. 获取状态

### `GET /status`

响应示例：

```json
{
  "config": {
    "sourceDir": "C:\\Users\\you\\Documents",
    "targetDir": "D:\\Backups\\LiteSync",
    "intervalMinutes": 30,
    "watchChanges": true
  },
  "status": {
    "running": false,
    "currentAction": "idle",
    "lastRunAt": "2026-03-11T06:00:00Z",
    "lastSuccessAt": "2026-03-11T06:00:05Z",
    "lastError": "",
    "lastTrigger": "schedule",
    "totalRuns": 12,
    "successRuns": 12,
    "failedRuns": 0,
    "nextScheduledRun": "2026-03-11T06:30:00Z"
  }
}
```

## 5. 获取日志

### `GET /logs?limit=120`

参数：

- `limit`（可选）：返回条数，范围 `1..1000`

响应示例：

```json
{
  "logs": [
    {
      "time": "2026-03-11T06:00:05Z",
      "level": "info",
      "message": "backup finished: 42 files, 123456 bytes -> D:\\Backups\\LiteSync\\snapshot-20260311-140000"
    }
  ]
}
```

## 6. 手动触发备份

### `POST /backup`

响应示例：

```json
{
  "status": "ok"
}
```

错误示例（任务正在运行）：

```json
{
  "error": "backup is already running"
}
```

