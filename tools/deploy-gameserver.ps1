# deploy-gameserver.ps1 - rebuild gameserver from this checkout and hot-replace the
# copy that the launcher runs. Keep this file ASCII-only: Windows PowerShell 5.1
# reads non-BOM .ps1 as ANSI and mangles non-ASCII paths.
#
# Usage:  powershell -ExecutionPolicy Bypass -File tools\deploy-gameserver.ps1 -ReleaseRoot <dir>
param(
    [Parameter(Mandatory = $true)][string]$ReleaseRoot,
    [string]$Panel = 'http://127.0.0.1:9500'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root

$go = 'F:\go\bin\go.exe'
if (-not (Test-Path $go)) { $go = 'C:\tools\go\bin\go.exe' }
$version = Get-Date -Format 'yyyyMMddHHmmss'
& $go build -trimpath -ldflags "-s -w -X 'rf4server/internal/buildinfo.Version=$version'" -o bin/gameserver.exe ./cmd/gameserver
if ($LASTEXITCODE -ne 0) { throw 'build failed' }

try { Invoke-WebRequest -Uri "$Panel/api/stop?name=gameserver" -Method Post -UseBasicParsing -TimeoutSec 10 | Out-Null } catch { }
Start-Sleep -Milliseconds 1200
$dst = Join-Path $ReleaseRoot 'bin\gameserver.exe'
if (Test-Path $dst) {
    $bak = "$dst.bak-$(Get-Date -Format 'yyyyMMddHHmmss')"
    Move-Item $dst $bak -Force
    Write-Host "backup -> $bak"
}
Copy-Item (Join-Path $root 'bin\gameserver.exe') $dst -Force
Write-Host "copied -> $dst"
try { Invoke-WebRequest -Uri "$Panel/api/start?name=gameserver" -Method Post -UseBasicParsing -TimeoutSec 15 | Out-Null } catch { }
Start-Sleep -Seconds 2
try { (Invoke-WebRequest -Uri 'http://127.0.0.1:9502/healthz' -UseBasicParsing -TimeoutSec 5).Content } catch { 'health check failed' }
Write-Host "version=$version"
