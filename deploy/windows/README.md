# Windows x64 使用说明 / Windows x64 quick start

适用于 64 位 Intel / AMD Windows。此包包含程序及完整管理页面，无需安装 Go、Node.js、Python、Docker 或 WSL。当前为前台运行版，不包含 Windows 系统服务、开机自启或自动升级。

## 首次使用

1. 从本项目 GitHub Release 下载 `codearts2api-panel-vX.Y.Z-windows-amd64.zip`，使用资源管理器“全部解压缩”到自己的文件夹；不要直接在压缩包中运行。
2. 双击 **start.cmd**，以普通用户运行即可，不需要管理员权限。保留命令行窗口。
3. 打开 **http://127.0.0.1:10087/**。默认仅本机可访问。
4. 当前源码构建的版本直接填写管理员邮箱和密码，无需 Token 或验证码。v0.3.7 发布包仍需从 `%LOCALAPPDATA%\CodeArts2API\state.json.setup.json` 读取初始化凭证，使用旧版向导。
5. 使用 `admin` 和刚设置的密码登录，在账号池添加自己的平台账号，在 API 密钥页面创建或查看调用密钥。普通用户由管理员创建。

启动器默认不导入本机 IDE 的账号。账号导入、登录和后续平台操作在管理页面中显式进行。若出现端口占用，不要反复启动；先检查是否已经有一个实例运行。

## 数据、停止与升级

使用 `start.cmd` 时，账号、配置、安装标记和用量账本统一保存在 **`%LOCALAPPDATA%\CodeArts2API`**。程序解压目录可以变化，数据目录保持不变。直接双击 `.exe` 则使用程序同目录的另一份默认状态；日常请统一使用 `start.cmd`，避免误以为原账号丢失。

- 停止：在运行窗口按 **Ctrl+C**，等待程序退出；若提示终止批处理，输入 `Y`。备份或升级前必须停止。
- 备份：退出程序后复制 **整个数据文件夹**，包括所有 `state.json.*` 文件，不只复制 `state.json`。数据包含账号凭据和密钥，应只保存在自己有权访问的位置。
- 升级：先停止并备份，阅读新版本说明，解压新包，再运行新包中的 `start.cmd`。同一 Windows 用户会继续使用原数据。不能同时启动新旧版本；跨 schema 降级不受支持。
- 卸载程序：删除解压的程序文件夹即可；数据独立保留，只有确认不再需要时再手动删除数据目录。

更换端口（命令提示符中执行）：

```bat
set CODEARTS_PORT=10088
start.cmd
```

手动运行及自定义状态位置（PowerShell，明确指定路径）：

```powershell
.\codearts2api-panel.exe -host 127.0.0.1 -port 10087 -data "$env:LOCALAPPDATA\CodeArts2API\state.json" -no-import
```

若曾使用源码版或直接运行 exe，先停止旧实例并备份，确认状态路径再迁移完整数据目录。一个数据目录只允许一个进程写入。不要只移走状态快照而留下安装标记，也不要手改版本号绕过迁移检查。

## 校验与发布范围

从同一 GitHub Release 下载 `SHA256SUMS`，在 PowerShell 执行下面的命令，将结果与清单中的对应 ZIP 摘要比较：

```powershell
Get-FileHash .\codearts2api-panel-vX.Y.Z-windows-amd64.zip -Algorithm SHA256
```

SHA-256 用于完整性校验，不是代码签名。程序目前没有 Authenticode 签名。Windows 或企业策略可能要求确认发布者；先核对下载来源和摘要，不能通过关闭系统防护来解决组织策略限制。

发布流水线在 Windows x64 runner 上实际运行解压后的启动器，验证安装凭证、设置密码、登录、权限边界、数据保存和换目录重启。它不代表所有 Windows 桌面版本、IDE 自动导入及真实上游账号操作均已逐一验收。

## English

Extract the Windows x64 ZIP and double-click **start.cmd** as a regular user. No Go, Node.js, Python, Docker, or WSL runtime is required. Open **http://127.0.0.1:10087/**. Current source asks for an administrator email and password without a setup token. Published v0.3.7 still requires the token from `state.json.setup.json` and uses the **admin** login.

Keep the console window open. Press **Ctrl+C** and wait for the process to exit before backing up the entire `%LOCALAPPDATA%\CodeArts2API` folder or upgrading. Extract the new ZIP and run its `start.cmd` under the same Windows account to reuse existing data. Do not run two copies against the same data. Launching the EXE directly uses a different default data location; use `start.cmd` consistently or pass an explicit `-data` path.

This is a foreground application, not a Windows service. Automatic startup, automatic upgrades, ARM64 packages, and Authenticode signing are not included. Verify the ZIP against `SHA256SUMS` from the same GitHub Release.
