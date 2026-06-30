# Builds the Go backend for Windows and names it for the Tauri sidecar:
#   src-tauri\binaries\flapp-core-<target-triple>.exe
# Run from any directory: powershell -ExecutionPolicy Bypass -File scripts\build-sidecar.ps1
$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$BinDir = Join-Path $Root "src-tauri\binaries"
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null

if (-not (Get-Command rustc -ErrorAction SilentlyContinue)) {
  Write-Error "rustc not found. Install the Rust toolchain (https://rustup.rs)."
}

$Triple = (rustc -Vv | Select-String '^host: ').ToString().Replace('host: ', '').Trim()
$Out = Join-Path $BinDir "flapp-core-$Triple.exe"

Write-Host "Building Go backend -> $Out"
Push-Location (Join-Path $Root "backend")
try {
  go mod tidy
  $env:CGO_ENABLED = "0"
  go build -trimpath -ldflags "-s -w" -o $Out ./cmd/flapp-core
} finally {
  Pop-Location
}
Write-Host "Done: $Out"
