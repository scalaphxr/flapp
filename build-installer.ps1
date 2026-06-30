#Requires -Version 5.1
<#
.SYNOPSIS
  Builds flapp NSIS installer for Windows.
  Output: src-tauri\target\release\bundle\nsis\flapp_*.exe
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$Root = $PSScriptRoot

function Step($msg) { Write-Host "`n==> $msg" -ForegroundColor Cyan }
function Ok($msg)   { Write-Host "    OK: $msg" -ForegroundColor Green }
function Fail($msg) { Write-Host "`nERROR: $msg" -ForegroundColor Red; exit 1 }

# ── 1. Prerequisites ─────────────────────────────────────────────────────────

Step "Checking prerequisites"

if (-not (Get-Command go    -ErrorAction SilentlyContinue)) { Fail "Go not found. Install from https://go.dev/dl/" }
if (-not (Get-Command cargo -ErrorAction SilentlyContinue)) { Fail "Rust/Cargo not found. Install from https://rustup.rs/" }
if (-not (Get-Command node  -ErrorAction SilentlyContinue)) { Fail "Node.js not found. Install from https://nodejs.org/" }

Ok "Go $(go version)"
Ok "Rust $(rustc --version)"
Ok "Node $(node --version)"

# ── 2. Install frontend deps if needed ───────────────────────────────────────

Step "Checking frontend dependencies"
$frontendModules = Join-Path $Root "frontend\node_modules"
if (-not (Test-Path $frontendModules)) {
    Write-Host "    Installing frontend deps..."
    Push-Location (Join-Path $Root "frontend")
    npm install
    if (-not $?) { Fail "npm install failed" }
    Pop-Location
} else {
    Ok "node_modules already present"
}

# ── 3. Build Go sidecar ──────────────────────────────────────────────────────

Step "Building Go sidecar (flapp-core)"
$triple  = (rustc -Vv | Select-String 'host:').ToString().Split()[-1].Trim()
$binDir  = Join-Path $Root "src-tauri\binaries"
$out     = Join-Path $binDir "flapp-core-$triple.exe"

New-Item -ItemType Directory -Force -Path $binDir | Out-Null

Push-Location (Join-Path $Root "backend")
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags "-s -w" -o $out ./cmd/flapp-core
if (-not $?) { Fail "Go build failed" }
Pop-Location
Remove-Item Env:\CGO_ENABLED -ErrorAction SilentlyContinue

Ok "Sidecar: $out"

# ── 4. Build frontend ────────────────────────────────────────────────────────

Step "Building React frontend"
Push-Location (Join-Path $Root "frontend")
npm run build
if (-not $?) { Fail "Frontend build failed" }
Pop-Location
Ok "Frontend built"

# ── 5. Build Tauri installer ─────────────────────────────────────────────────

Step "Building Tauri NSIS installer (this takes a few minutes)"
$tauriBin = Join-Path $Root "frontend\node_modules\.bin\tauri.cmd"
if (-not (Test-Path $tauriBin)) {
    $tauriBin = Join-Path $Root "frontend\node_modules\.bin\tauri"
}
Push-Location $Root
& $tauriBin build --bundles nsis
if (-not $?) { Fail "Tauri build failed" }
Pop-Location

# ── 6. Locate and report output ──────────────────────────────────────────────

Step "Locating installer"
$nsisDir = Join-Path $Root "src-tauri\target\release\bundle\nsis"
$installer = Get-ChildItem $nsisDir -Filter "*.exe" -ErrorAction SilentlyContinue | Select-Object -First 1

if ($installer) {
    Write-Host ""
    Write-Host "  Installer ready:" -ForegroundColor Yellow
    Write-Host "  $($installer.FullName)" -ForegroundColor White
    Write-Host "  Size: $([math]::Round($installer.Length / 1MB, 1)) MB" -ForegroundColor White
    Write-Host ""
    Write-Host "  Copy this file to another PC and run it to install flapp." -ForegroundColor Green
} else {
    Fail "Installer not found in $nsisDir"
}
