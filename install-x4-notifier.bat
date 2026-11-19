@echo off
rem Installs the Fleet Order Notifier extension into X4: Foundations.
setlocal
cd /d "%~dp0"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0scripts\install.ps1" %*
echo.
pause
