@echo off
setlocal
chcp 65001 >nul
cd /d "%~dp0"

set "CLIENT=%~dp0release\windows-x64\SvnEasyClient.exe"
set "CONFIG=%~dp0svneasy-client.json"

if not exist "%CLIENT%" set "CLIENT=%~dp0SvnEasyClient.exe"

if not exist "%CLIENT%" (
  echo 找不到客户端程序：%CLIENT%
  pause
  exit /b 1
)

if not exist "%CONFIG%" (
  echo 首次使用，请设置 SVN 工作副本和自动追踪白名单。
  echo.
  "%CLIENT%" init --config "%CONFIG%"
  if errorlevel 1 (
    echo.
    echo 创建配置失败，请查看上方错误。
    pause
    exit /b 1
  )
)

echo 正在检查配置、自动安装 SVN 命令行工具并启用后台追踪...
"%CLIENT%" install --config "%CONFIG%"
if errorlevel 1 (
  echo.
  echo 安装失败，请查看上方错误。
  pause
  exit /b 1
)

echo.
echo 完成。以后登录 Windows 后会自动追踪白名单目录。
pause
