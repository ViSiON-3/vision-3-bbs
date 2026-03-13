# ViSiON/3 BBS Build Script (Windows)
#
# Compiles all ViSiON/3 binaries. Run .\setup.ps1 first for initial installation.
#
# Usage: .\build.ps1
#
# If running scripts is disabled on your system, use build.bat instead, or run:
#   powershell -ExecutionPolicy Bypass -File build.ps1

$ErrorActionPreference = "Stop"

$scriptRoot = $PSScriptRoot
if (-not $scriptRoot) { $scriptRoot = Get-Location }
Set-Location $scriptRoot

Write-Host "=== Building ViSiON/3 BBS ===" -ForegroundColor Cyan
$BUILT = @()

$goCmd = Get-Command go -ErrorAction SilentlyContinue
if (-not $goCmd) {
    Write-Host "Go executable not found in PATH. Re-run setup.ps1 or reopen your shell." -ForegroundColor Red
    exit 1
}
$goExe = $goCmd.Source

$targets = @(
    @{ Cmd = "vision3"; Desc = "BBS server" },
    @{ Cmd = "helper"; Desc = "helper process" },
    @{ Cmd = "v3mail"; Desc = "mail processor" },
    @{ Cmd = "strings"; Desc = "strings editor" },
    @{ Cmd = "ue"; Desc = "user editor" },
    @{ Cmd = "config"; Desc = "config editor" },
    @{ Cmd = "menuedit"; Desc = "menu editor" }
)
foreach ($t in $targets) {
    $exe = $t.Cmd + ".exe"
    & $goExe build -o $exe "./cmd/$($t.Cmd)"
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Build failed ($($t.Cmd))!" -ForegroundColor Red
        exit 1
    }
    $BUILT += "  $exe - $($t.Desc)"
}

Write-Host "============================="
Write-Host "Build successful!" -ForegroundColor Green
Write-Host ""
foreach ($item in $BUILT) { Write-Host $item }
Write-Host ""
