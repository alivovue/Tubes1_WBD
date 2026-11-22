@echo off
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0run-dev.ps1" %*
exit /b %errorlevel%
