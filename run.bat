@echo off
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0run.ps1"
set "schakker_exit_code=%errorlevel%"
if not "%schakker_exit_code%"=="0" pause
exit /b %schakker_exit_code%
