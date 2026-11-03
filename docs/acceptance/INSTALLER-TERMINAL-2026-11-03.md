# 终端安装向导验收（2026-11-03）

## 范围

在既有 `deploy/install.sh` 中加入控制终端交互。参考 [Sub2API install.sh](https://github.com/Wei-Shaw/sub2api/blob/main/deploy/install.sh) 的 `/dev/tty` 方式，独立实现 Python 提问、校验与取消；既有安装工具、校验来源、数据格式和服务管理逻辑不变。

系统级新安装有控制终端时自动询问语言、地址、端口及确认；无终端沿用参数和默认值。`--interactive` 强制要求终端，`--non-interactive` 禁止提问；显式地址/端口不重复提问。语言只影响安装器，不修改面板语言。v0.3.8 旧附件中的维护工具诊断仍可能使用中文。

## 已执行

- `sh -n deploy/install.sh`、`git diff --check`：通过。
- `python3 -m unittest discover -s deploy/tests -v`：65 项通过，其中安装器相关 14 项。
- 真正的控制 PTY + `bash -s` 管道测试：英文选择、全网卡地址、8089 端口通过 SHA-256 验证后传入独立维护子进程；只替换 GitHub 响应、权限/平台前提和维护程序内容，不创建真实系统服务。
- 真实脚本取消测试：回答 n、Ctrl+C、EOF 均退出 130，无网络下载，无 traceback。
- 无控制终端、显式无人值守、显式参数优先、非法输入重试、强制交互缺少终端、已有目录拒绝与摘要不匹配拒绝：通过。
- `python3 deploy/tests/installer_acceptance.py --archive artifacts/releases/v0.3.8-verification/codearts2api-panel-v0.3.8-linux-amd64.tar.gz`：隔离的用户级 systemd 实例完成真实包安装、首次初始化、邮箱管理员登录及实际进程版本检查。测试单元随后清理，使用本地 Release 数据替代 GitHub 下载。

## 边界

本次未卸载或更新现有 `/opt/codearts2api` 服务，没有修改真实账号、源码数据或备份。未在本机执行需要 root 的系统服务安装验收；系统模式参数传递用 PTY 夹具验证，已有专用用户/权限/升级测试保持通过。未增加 GitHub 限流回退或发布新版本，v0.3.8 附件保持不变；新向导由 main 分支的安装入口提供。


## 同日补充：系统语言与终端配色

- 默认按 LC_ALL → LC_MESSAGES → LANG 判断；回车跟随系统，菜单 / --lang 明确选择优先并在本次执行保持。新增 --lang auto。
- 帮助、参数错误、安装进度与完成摘要使用同一语言；兼容 v0.3.8 旧维护工具，成功摘要由入口统一呈现，失败原始诊断保留。
- ANSI 标题青色、进度蓝色、成功绿色、警告黄色、错误红色；非 TTY、NO_COLOR、TERM=dumb 输出纯文本。
- 新增真实 PTY 测试：英文系统回车走英文、英文系统明确选择中文、所选语言覆盖到最终摘要、颜色启停、帮助/参数错误语言一致、子进程失败诊断不丢失。
- 安装语言只影响当前终端过程；网页继续按现有浏览器语言偏好规则独立处理，未改变浏览器存储或服务端账号数据。
- 补充验证结果：70 项部署测试通过（安装器 19 项）；C.UTF-8 环境下隔离真实包安装、初始化、管理员登录通过，安装入口至完成摘要均为英文。
