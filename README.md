# SVN Easy Kit

让 SVN 的日常操作简单一点：

- 客户端自动登记指定项目中的新增、修改和删除。
- 服务端快速创建独立仓库，并复制旧仓库的用户和权限。
- 原生单文件程序，不需要 Python、Node.js 或 .NET。
- 不自动提交代码，提交前仍由用户确认。

## 客户端：双击即可

### Windows

1. 从 [Releases](https://github.com/redtidev1918/svn-easy-kit/releases) 下载 Windows 压缩包并解压。
2. 双击 `Start-SvnEasy-Windows.cmd` 或 `SvnEasyClient.exe`。
3. 按编号选择工作副本和项目，一路回车接受推荐设置。

程序会自动：

- 搜索桌面、文档和常见项目目录中的 SVN 工作副本。
- 识别常见项目文件及 `Source`、`src`、`Config`、`Content`、`assets` 等目录。
- 缺少 SVN 命令行工具时自动安装。
- 启用登录后后台追踪。
- 创建开始菜单快捷方式 `SVN Easy Client`。

以后可以从开始菜单搜索 `SVN Easy Client`，会看到：

```text
1. 同步变化并打开提交窗口
2. 修改追踪项目或目录
3. 修复后台自动追踪
4. 检查状态
5. 关闭后台自动追踪
```

### Linux / macOS 客户端

运行程序即可进入相同向导：

```bash
./SvnEasyClient
```

Linux 使用 systemd 用户服务，macOS 使用 LaunchAgent。

## 客户端会做什么

- 修改文件：SVN 自动识别。
- 新建文件：白名单内自动登记为新增。
- 删除文件：白名单内自动登记为删除。
- 白名单外内容：不会自动处理。
- 提交：始终需要用户确认。

重命名若需要保留移动历史，请使用 SVN 重命名或 TortoiseSVN 的“修复移动”。

## 服务端：菜单操作

下载对应 Linux 发布包并解压：

```bash
chmod +x install-server-linux.sh
sudo ./install-server-linux.sh
```

程序会自动识别：

- x64 或 ARM64
- Subversion 是否已安装
- `svnserve -r` 指定的仓库根目录
- 已有仓库列表

进入菜单后，通常只需要：

1. 选择“创建新仓库”。
2. 用编号选择一个旧仓库作为权限模板。
3. 输入新仓库名。
4. 回车确认。

程序会自动创建独立仓库、复制用户与权限、调整仓库名权限段，并创建：

```text
trunk/
branches/
tags/
```

同一菜单也能新增用户和修改读写权限。修改认证文件前会自动备份。

## 高级命令

不使用菜单时也可以执行：

```bash
SvnEasyClient sync --config ./svneasy-client.json

svneasy-server create --from template-repo --name new-repo
svneasy-server user --repo new-repo --name alice --access rw
svneasy-server permission --repo new-repo --principal alice --path /trunk --access rw
```

查看全部参数：

```bash
SvnEasyClient help
svneasy-server help
```

## 支持平台

- Windows x64
- Linux x64 / ARM64
- macOS Intel / Apple Silicon

## 从源码构建

需要 Go 1.22 或更高版本：

```bash
go test ./...
go build ./cmd/svneasy-client
go build ./cmd/svneasy-server
```

MIT License
