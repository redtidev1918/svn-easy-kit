@echo off
setlocal
chcp 65001 >nul
cd /d "%~dp0"

set "CLIENT=%~dp0release\windows-x64\SvnEasyClient.exe"
if not exist "%CLIENT%" set "CLIENT=%~dp0SvnEasyClient.exe"

"%CLIENT%" uninstall --config "%~dp0svneasy-client.json"
echo 已移除开机后台启动。SVN 数据、配置和项目文件均未删除。
pause
