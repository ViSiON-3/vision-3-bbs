@echo off
REM ViSiON/3 BBS Build — Launches build.ps1 with the correct execution policy.
REM If PowerShell blocks build.ps1, run this file instead: build.bat
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1"
