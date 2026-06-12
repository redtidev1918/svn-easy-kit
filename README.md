# SVN Easy Kit

一套小巧的 SVN 客户端与服务端辅助工具。发布包是原生单文件程序，不要求用户安装 Python、Node.js 或 .NET。

- `SvnEasyClient`：监控用户指定的白名单路径，自动登记新增和删除，避免每次手动执行 `svn add`、`svn delete`。
- `SvnEasyServer`：创建独立仓库，复制模板仓库的用户和权限配置，并提供用户与路径权限管理。

工具不会自动提交代码。所有变化仍由用户检查并提交。

## 下载

从 [GitHub Releases](https://github.com/redtidev1918/svn-easy-kit/releases) 下载对应平台的压缩包：

- Windows x64
- Linux x64 / ARM64
- macOS Intel / Apple Silicon

## 客户端快速开始

### Windows

1. 解压 Windows 发布包。
2. 双击 `install-client-windows.cmd`。
3. 首次运行时根据提示填写 SVN 工作副本和白名单路径。

安装程序会自动：

- 检查 SVN 命令行客户端。
- 缺少 SVN 时通过 `winget` 安装。
- 执行首次同步。
- 注册为用户登录后后台运行。

需要提交时双击：

```text
sync-and-commit-windows.cmd
```

它会先同步白名单，再打开 TortoiseSVN 提交窗口。

### Linux 与 macOS

先生成配置：

```bash
./SvnEasyClient init --config ./svneasy-client.json
```

检查并安装后台服务：

```bash
./SvnEasyClient install --config ./svneasy-client.json
```

Linux 使用 systemd 用户服务，macOS 使用 LaunchAgent。

## 客户端配置

配置向导会询问三个核心项目：

1. `workingCopy`：包含 `.svn` 的工作副本根目录。
2. `scanRoot`：提交和状态扫描的项目目录。
3. `targets`：允许自动登记新增与删除的白名单路径。

通用示例：

```json
{
  "workingCopy": "D:\\SVN\\Workspace",
  "scanRoot": "GameProject",
  "targets": [
    "GameProject\\Source",
    "GameProject\\Config",
    "GameProject\\Content",
    "GameProject\\GameProject.uproject"
  ],
  "pollSeconds": 2,
  "respectSvnIgnore": false,
  "autoDelete": true,
  "logFile": "svneasy-client.log"
}
```

相对路径均以 `workingCopy` 为基准。白名单可用于任何项目结构，不限于 Unreal Engine。

### 客户端行为

- 已修改文件：由 SVN 原生识别。
- 新文件或目录：在白名单内自动执行 `svn add`。
- 已删除文件或目录：在白名单内自动执行 `svn delete --force`。
- 白名单外路径：不会自动添加或删除。
- `respectSvnIgnore: false`：显式白名单优先，即使文件命中忽略规则也会登记。
- `respectSvnIgnore: true`：继续遵守 SVN 忽略规则。

文件系统中的重命名通常会显示为“删除旧文件 + 新增新文件”。需要保留移动历史时，请使用 SVN 的重命名功能，或在 TortoiseSVN 提交窗口中使用“修复移动”。

### 常用命令

```bash
SvnEasyClient init      --config ./svneasy-client.json
SvnEasyClient doctor    --config ./svneasy-client.json
SvnEasyClient sync      --config ./svneasy-client.json
SvnEasyClient watch     --config ./svneasy-client.json
SvnEasyClient commit    --config ./svneasy-client.json
SvnEasyClient install   --config ./svneasy-client.json
SvnEasyClient uninstall --config ./svneasy-client.json
```

## 服务端快速开始

### Linux 一键安装

解压 Linux 发布包后，以具备仓库目录写权限的用户运行：

```bash
chmod +x install-server-linux.sh
sudo ./install-server-linux.sh
```

工具会检测 CPU 架构、Subversion 和正在运行的 `svnserve -r` 参数，然后进入交互菜单。

如果无法自动识别仓库根目录，可使用：

```bash
svneasy-server doctor --root /path/to/repositories
```

### 创建独立仓库并迁移权限

从现有仓库复制认证配置：

```bash
svneasy-server create \
  --from template-repo \
  --name new-repo \
  --layout=true
```

同时新增用户：

```bash
svneasy-server create \
  --from template-repo \
  --name new-repo \
  --user alice \
  --access rw
```

没有通过 `--password` 明文传入密码时，程序会提示隐藏输入。

### 用户与权限管理

新增或更新用户：

```bash
svneasy-server user \
  --repo new-repo \
  --name bob \
  --access rw
```

设置用户权限：

```bash
svneasy-server permission \
  --repo new-repo \
  --principal bob \
  --path /trunk \
  --access rw
```

设置组权限：

```bash
svneasy-server permission \
  --repo new-repo \
  --principal @developers \
  --path /branches \
  --access r
```

撤销访问：

```bash
svneasy-server permission \
  --repo new-repo \
  --principal bob \
  --path /private \
  --access none
```

### 服务端迁移内容

- 创建真正独立的 SVN repository。
- 复制 `svnserve.conf` 和相关认证配置。
- 识别相对或绝对路径的 `passwd`、`authz`、`groups-db`。
- 将模板仓库的命名权限段改为新仓库名称。
- 为新仓库创建独立认证文件，避免后续修改影响模板仓库。
- 在 Linux 上复制模板仓库的所有者和组。
- 可自动创建 `trunk`、`branches`、`tags`。
- 修改认证文件前创建带时间戳的备份。

新建仓库通常不需要重启 `svnserve`。

## 安全说明

- 客户端只登记工作副本变化，不自动提交。
- 服务端修改认证文件前会备份原文件。
- 建议避免通过命令行参数传入密码，因为命令可能进入 shell 历史；直接等待隐藏密码提示更安全。
- 首次用于生产服务器前，建议先备份仓库目录。

## 从源码构建

需要 Go 1.22 或更高版本：

```bash
go test ./...
go build ./cmd/svneasy-client
go build ./cmd/svneasy-server
```

项目使用 MIT License。
