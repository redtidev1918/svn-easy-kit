# SVN Easy Kit

面向 SVN 初学者的客户端和服务器辅助工具。程序不会替用户自动提交代码。

## Windows 客户端

下载 Windows 发布包、解压，然后双击：

```text
Start-SvnEasy-Windows.cmd
```

### 第一次上传本地项目

选择：

```text
1. 第一次把本地项目上传到一个空的 SVN 仓库
```

程序会：

1. 打开文件夹选择窗口，由用户亲自选择项目。
2. 要求粘贴远程 `trunk` 地址。
3. 显示完整操作预览。
4. 检查远程 `trunk` 是否为空。
5. 将本地目录变成 SVN 工作副本。
6. 设置忽略规则。
7. 登记需要版本控制的文件。
8. 打开 TortoiseSVN 提交窗口。

最后仍需用户在 TortoiseSVN 中点击“确定”，文件才会上传。

### Unreal Engine 项目

检测到 `.uproject` 后，只推荐：

```text
Config
Content
Source
项目名.uproject
```

自动忽略：

```text
.idea
.vs
Binaries
DerivedDataCache
Intermediate
Saved
*.sln
.vsconfig
*.DotSettings.user
```

工具不会扫描桌面或文档目录，也不会猜测项目位置。

### 日常使用

安装后可从开始菜单打开 `SVN Easy Client`：

```text
1. 提交今天的修改
2. 从服务器更新到最新版本
3. 查看有哪些文件发生了变化
4. 更换项目或重新选择追踪内容
5. 修复后台自动追踪
6. 关闭后台自动追踪
```

状态使用“新增、修改、删除、冲突”等普通语言显示，不要求用户理解 `A/M/D/?`。

## Linux 服务端

解压 Linux 发布包后运行：

```bash
chmod +x install-server-linux.sh
sudo ./install-server-linux.sh
```

菜单可以：

- 创建独立仓库
- 从旧仓库复制用户和权限
- 自动创建 `trunk/branches/tags`
- 新增或修改用户
- 修改读写权限

创建仓库时只需选择模板仓库、输入新仓库名并确认。

## 安全原则

- 不自动搜索本地项目。
- 执行前显示本地路径、远程地址、提交项和忽略项。
- 远程 `trunk` 非空时拒绝执行第一次上传。
- 首次连接失败时删除新生成的 `.svn`，不删除项目文件。
- 不自动提交，最终上传必须由用户确认。
- 服务端修改认证配置前创建备份。

## 高级命令

```bash
SvnEasyClient preview --config ./svneasy-client.json
SvnEasyClient update --config ./svneasy-client.json
SvnEasyClient commit --config ./svneasy-client.json

svneasy-server create --from template-repo --name new-repo
svneasy-server user --repo new-repo --name alice --access rw
```

## 支持平台

- Windows x64
- Linux x64 / ARM64
- macOS Intel / Apple Silicon

## 构建

需要 Go 1.22 或更高版本：

```bash
go test ./...
go build ./cmd/svneasy-client
go build ./cmd/svneasy-server
```

MIT License
