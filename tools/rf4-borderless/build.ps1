# 构建 rf4-borderless.exe（需要 MinGW-w64 的 g++ 在 PATH 中）
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path

$compiler = Get-Command g++ -ErrorAction SilentlyContinue
if (-not $compiler) {
    throw '未找到 g++，请先把 MinGW-w64 的 bin 目录加入 PATH'
}

$outDir = Join-Path $root 'bin'
New-Item -ItemType Directory -Force -Path $outDir | Out-Null
$outFile = Join-Path $outDir 'rf4-borderless.exe'

& $compiler.Source -std=c++17 -O2 -Wall -Wextra -static -o $outFile `
    (Join-Path $root 'src\borderless.cpp') `
    (Join-Path $root 'src\win32_guard.cpp') `
    (Join-Path $root 'src\main.cpp')
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "输出: $outFile"
