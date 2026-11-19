@echo off
rem Starts the X4 Fleet notifier server using the .env file next to this script.
cd /d "%~dp0"
x4-notifier-windows-amd64.exe
echo.
echo Server stopped. Press any key to close this window.
pause >nul
