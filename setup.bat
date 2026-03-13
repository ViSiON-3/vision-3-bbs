@echo off
REM ViSiON/3 BBS Setup — Launches setup.ps1 with the correct execution policy.
REM If PowerShell blocks setup.ps1, run this file instead: setup.bat
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0setup.ps1"
