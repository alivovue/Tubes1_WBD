@echo off
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0run.ps1" -Worker %*
exit /b %errorlevel%
