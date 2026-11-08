# 编译并运行无边框判定逻辑的单元测试（不依赖 Windows API）
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $MyInvocation.MyCommand.Path

$compiler = Get-Command g++ -ErrorAction SilentlyContinue
if (-not $compiler) {
    throw '未找到 g++，请先把 MinGW-w64 的 bin 目录加入 PATH'
}

$outFile = Join-Path $root 'tests\borderless_test.exe'
& $compiler.Source -std=c++17 -O2 -Wall -Wextra -I (Join-Path $root 'src') -o $outFile `
    (Join-Path $root 'tests\borderless_test.cpp') `
    (Join-Path $root 'src\borderless.cpp')
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

& $outFile
exit $LASTEXITCODE
