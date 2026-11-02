@echo off
setlocal
cd /d "%~dp0" || (pause & exit /b 1)
title Agent-Go - DeepSeek Review
powershell.exe -NoProfile -STA -ExecutionPolicy Bypass -File "%~dp0start-review.ps1"
set "result=%errorlevel%"
pause
exit /b %result%
