@echo off
setlocal
chcp 65001 >nul
cd /d "%~dp0"

set "CLIENT=%~dp0release\windows-x64\SvnEasyClient.exe"
set "CONFIG=%~dp0svneasy-client.json"

if not exist "%CLIENT%" set "CLIENT=%~dp0SvnEasyClient.exe"

if not exist "%CONFIG%" (
  echo 尚未创建配置，正在启动配置向导...
  "%CLIENT%" init --config "%CONFIG%"
  if errorlevel 1 (
    pause
    exit /b 1
  )
)

"%CLIENT%" commit --config "%CONFIG%"
if errorlevel 1 pause
