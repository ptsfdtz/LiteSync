param(
  [string]$OutputDir = "release",
  [switch]$WithConsole
)

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $PSScriptRoot
$ClientDir = Join-Path $RepoRoot "client"
$ServerDir = Join-Path $RepoRoot "server"
$ClientDistDir = Join-Path $ClientDir "apps\web\dist"
$EmbeddedDistDir = Join-Path $ServerDir "internal\webui\dist"
$ReleaseDir = Join-Path $RepoRoot $OutputDir
$OutputExe = Join-Path $ReleaseDir "litesync.exe"

Write-Host "==> Build web UI"
Push-Location $ClientDir
try {
  $env:CI = "true"
  pnpm install --frozen-lockfile
  pnpm --filter web build
}
finally {
  Pop-Location
}

if (!(Test-Path $ClientDistDir)) {
  throw "Web build output not found: $ClientDistDir"
}

Write-Host "==> Sync web assets into Go embed directory"
if (Test-Path $EmbeddedDistDir) {
  Remove-Item -Recurse -Force $EmbeddedDistDir
}
New-Item -ItemType Directory -Force -Path $EmbeddedDistDir | Out-Null
Copy-Item -Recurse -Force (Join-Path $ClientDistDir "*") $EmbeddedDistDir

Write-Host "==> Build standalone exe"
New-Item -ItemType Directory -Force -Path $ReleaseDir | Out-Null
Push-Location $ServerDir
try {
  go mod tidy
  if ($WithConsole) {
    go build -o $OutputExe ./cmd/litesync-server
  } else {
    go build -ldflags "-H=windowsgui" -o $OutputExe ./cmd/litesync-server
  }
}
finally {
  Pop-Location
}

Write-Host ""
Write-Host "Build complete:"
Write-Host "  $OutputExe"
if ($WithConsole) {
  Write-Host "  (console mode enabled)"
} else {
  Write-Host "  (windowless mode: no terminal popup)"
}
Write-Host ""
Write-Host "Run:"
Write-Host "  $OutputExe"
