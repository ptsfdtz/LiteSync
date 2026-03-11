import { useEffect, useMemo, useState } from "react"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@workspace/ui/components/card"
import { Input } from "@workspace/ui/components/input"
import { Label } from "@workspace/ui/components/label"
import { Switch } from "@workspace/ui/components/switch"
import { Textarea } from "@workspace/ui/components/textarea"

type AppConfig = {
  sourceDir: string
  targetDir: string
  intervalMinutes: number
  watchChanges: boolean
}

type RuntimeStatus = {
  running: boolean
  currentAction?: string
  lastRunAt?: string
  lastSuccessAt?: string
  lastError?: string
  lastTrigger?: string
  totalRuns: number
  successRuns: number
  failedRuns: number
  nextScheduledRun?: string
}

type StatusResponse = {
  config: AppConfig
  status: RuntimeStatus
}

type LogEntry = {
  time: string
  level: string
  message: string
}

const defaultConfig: AppConfig = {
  sourceDir: "",
  targetDir: "",
  intervalMinutes: 60,
  watchChanges: false,
}

const defaultStatus: RuntimeStatus = {
  running: false,
  totalRuns: 0,
  successRuns: 0,
  failedRuns: 0,
}

const apiBaseURL = (import.meta.env.VITE_API_BASE_URL || "/api").replace(
  /\/+$/,
  ""
)

function apiURL(path: string) {
  return `${apiBaseURL}/${path.replace(/^\/+/, "")}`
}

async function requestJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(apiURL(path), {
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers || {}),
    },
    ...init,
  })

  const payload = await response.json().catch(() => ({}))
  if (!response.ok) {
    const message =
      typeof payload?.error === "string"
        ? payload.error
        : `Request failed (${response.status})`
    throw new Error(message)
  }

  return payload as T
}

function formatDateTime(value?: string) {
  if (!value) {
    return "-"
  }

  return new Date(value).toLocaleString()
}

function statusVariant(
  status: RuntimeStatus
): "default" | "secondary" | "destructive" {
  if (status.running) {
    return "default"
  }
  if (status.lastError) {
    return "destructive"
  }
  return "secondary"
}

export function App() {
  const [config, setConfig] = useState<AppConfig>(defaultConfig)
  const [status, setStatus] = useState<RuntimeStatus>(defaultStatus)
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [triggering, setTriggering] = useState(false)
  const [message, setMessage] = useState("")
  const [error, setError] = useState("")

  const renderedLogs = useMemo(() => {
    if (logs.length === 0) {
      return "暂无日志。保存配置后，可点击“立即备份”或等待调度触发。"
    }

    return logs
      .map((entry) => {
        const stamp = new Date(entry.time).toLocaleString()
        return `[${stamp}] [${entry.level.toUpperCase()}] ${entry.message}`
      })
      .join("\n")
  }, [logs])

  async function loadConfig() {
    const nextConfig = await requestJSON<AppConfig>("config")
    setConfig(nextConfig)
  }

  async function refreshStatusAndLogs() {
    const [statusPayload, logsPayload] = await Promise.all([
      requestJSON<StatusResponse>("status"),
      requestJSON<{ logs: LogEntry[] }>("logs?limit=120"),
    ])

    setStatus(statusPayload.status)
    setLogs(logsPayload.logs)
  }

  useEffect(() => {
    let cancelled = false

    const run = async () => {
      setLoading(true)
      setError("")
      try {
        await Promise.all([loadConfig(), refreshStatusAndLogs()])
      } catch (loadError) {
        if (!cancelled) {
          setError(
            loadError instanceof Error ? loadError.message : "初始化失败"
          )
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
        }
      }
    }

    void run()

    const timer = window.setInterval(() => {
      void refreshStatusAndLogs().catch(() => undefined)
    }, 5000)

    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [])

  async function handleSaveConfig() {
    setSaving(true)
    setMessage("")
    setError("")
    try {
      const saved = await requestJSON<AppConfig>("config", {
        method: "PUT",
        body: JSON.stringify(config),
      })
      setConfig(saved)
      await refreshStatusAndLogs()
      setMessage("配置已保存，调度器与监听器已更新。")
    } catch (saveError) {
      setError(saveError instanceof Error ? saveError.message : "保存失败")
    } finally {
      setSaving(false)
    }
  }

  async function handleManualBackup() {
    setTriggering(true)
    setMessage("")
    setError("")
    try {
      await requestJSON("backup", { method: "POST" })
      await refreshStatusAndLogs()
      setMessage("已触发立即备份。")
    } catch (triggerError) {
      setError(
        triggerError instanceof Error ? triggerError.message : "触发失败"
      )
    } finally {
      setTriggering(false)
    }
  }

  return (
    <div className="mx-auto flex min-h-svh w-full max-w-6xl flex-col gap-6 p-4 md:p-8">
      <Card>
        <CardHeader className="gap-3">
          <CardTitle className="text-2xl">LiteSync 自动备份服务</CardTitle>
          <CardDescription>
            本地服务已启动后，可在这里配置备份源目录、目标目录、调度频率与文件变更自动触发。
          </CardDescription>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={statusVariant(status)}>
              {status.running ? "备份进行中" : "空闲"}
            </Badge>
            {status.lastError ? (
              <Badge variant="destructive">最近执行失败</Badge>
            ) : (
              <Badge variant="secondary">最近执行正常</Badge>
            )}
          </div>
        </CardHeader>
      </Card>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>备份配置</CardTitle>
            <CardDescription>
              目录路径建议使用绝对路径。保存后会立即写入本地配置文件，并自动应用。
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="grid gap-2">
              <Label htmlFor="sourceDir">源目录</Label>
              <Input
                id="sourceDir"
                placeholder="例如: C:\\Users\\you\\Documents"
                value={config.sourceDir}
                onChange={(event) =>
                  setConfig((prev) => ({
                    ...prev,
                    sourceDir: event.target.value,
                  }))
                }
              />
            </div>

            <div className="grid gap-2">
              <Label htmlFor="targetDir">目标目录</Label>
              <Input
                id="targetDir"
                placeholder="例如: D:\\Backups\\LiteSync"
                value={config.targetDir}
                onChange={(event) =>
                  setConfig((prev) => ({
                    ...prev,
                    targetDir: event.target.value,
                  }))
                }
              />
            </div>

            <div className="grid gap-2">
              <Label htmlFor="intervalMinutes">定时频率（分钟）</Label>
              <Input
                id="intervalMinutes"
                type="number"
                min={1}
                value={config.intervalMinutes}
                onChange={(event) =>
                  setConfig((prev) => ({
                    ...prev,
                    intervalMinutes: Number(event.target.value) || 1,
                  }))
                }
              />
            </div>

            <div className="flex items-center justify-between rounded-lg border p-3">
              <div className="grid gap-1">
                <Label htmlFor="watchChanges">文件变化自动触发</Label>
                <p className="text-xs text-muted-foreground">
                  开启后，源目录文件有变化将自动触发备份（带防抖）。
                </p>
              </div>
              <Switch
                id="watchChanges"
                checked={config.watchChanges}
                onCheckedChange={(checked) =>
                  setConfig((prev) => ({ ...prev, watchChanges: checked }))
                }
              />
            </div>

            <div className="flex flex-wrap gap-2">
              <Button disabled={saving || loading} onClick={handleSaveConfig}>
                {saving ? "保存中..." : "保存配置"}
              </Button>
              <Button
                variant="outline"
                disabled={triggering || loading}
                onClick={handleManualBackup}
              >
                {triggering ? "触发中..." : "立即备份"}
              </Button>
              <Button
                variant="ghost"
                disabled={loading}
                onClick={() => void refreshStatusAndLogs()}
              >
                刷新状态
              </Button>
            </div>

            {message ? (
              <p className="text-sm text-green-600 dark:text-green-400">
                {message}
              </p>
            ) : null}
            {error ? (
              <p className="text-sm text-red-600 dark:text-red-400">{error}</p>
            ) : null}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>任务状态</CardTitle>
            <CardDescription>
              展示最近运行结果、调度状态和统计信息。
            </CardDescription>
          </CardHeader>
          <CardContent className="grid gap-3 text-sm">
            <div className="grid grid-cols-2 gap-2 rounded-lg border p-3">
              <p className="text-muted-foreground">当前动作</p>
              <p className="text-right">{status.currentAction || "-"}</p>
              <p className="text-muted-foreground">最近触发方式</p>
              <p className="text-right">{status.lastTrigger || "-"}</p>
              <p className="text-muted-foreground">下次定时执行</p>
              <p className="text-right">
                {formatDateTime(status.nextScheduledRun)}
              </p>
            </div>

            <div className="grid grid-cols-2 gap-2 rounded-lg border p-3">
              <p className="text-muted-foreground">总执行次数</p>
              <p className="text-right">{status.totalRuns}</p>
              <p className="text-muted-foreground">成功次数</p>
              <p className="text-right">{status.successRuns}</p>
              <p className="text-muted-foreground">失败次数</p>
              <p className="text-right">{status.failedRuns}</p>
            </div>

            <div className="grid grid-cols-2 gap-2 rounded-lg border p-3">
              <p className="text-muted-foreground">最近执行时间</p>
              <p className="text-right">{formatDateTime(status.lastRunAt)}</p>
              <p className="text-muted-foreground">最近成功时间</p>
              <p className="text-right">
                {formatDateTime(status.lastSuccessAt)}
              </p>
            </div>

            {status.lastError ? (
              <div className="rounded-lg border border-red-400/50 bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">
                最近错误: {status.lastError}
              </div>
            ) : null}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>最近日志</CardTitle>
          <CardDescription>
            实时显示最新执行日志，默认每 5 秒自动刷新一次。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Textarea
            value={renderedLogs}
            readOnly
            className="min-h-80 font-mono text-xs"
          />
        </CardContent>
      </Card>
    </div>
  )
}
