@echo off
setlocal
chcp 65001 >nul
cd /d "%~dp0"

if not exist "%~dp0SvnEasyClient.exe" (
  echo SvnEasyClient.exe not found.
  pause
  exit /b 1
)

"%~dp0SvnEasyClient.exe"
if errorlevel 1 pause
