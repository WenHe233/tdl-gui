# TDL Media

TDL Media 是一个 Windows 优先的 Telegram 媒体浏览与批量下载器。它使用官方发布的 [tdl](https://github.com/iyear/tdl) 作为下载引擎，提供可保存的筛选规则、固定下载清单、同聊天媒体去重、任务恢复和 Telegram 风格桌面界面。

## 快速开始

解压便携包后：

1. 运行 `tdl-media-gui.exe`。
2. 首次启动时安装官方 tdl v0.20.4，或指定已有的 `tdl.exe`。
3. 添加账户并选择扫码、手机号验证码或 Telegram Desktop 导入。
4. 刷新聊天列表，选择聊天后点击“扫描媒体”。
5. 设置日期、类型、大小和总量限制，生成下载清单。
6. 在清单中排除不需要的项目，然后开始下载。

账户会话、媒体索引和任务记录默认保存在 `%APPDATA%\TDL Media`。下载默认保存到 `%USERPROFILE%\Downloads\Telegram Media`。可通过 CLI 的 `--data-dir` 或环境变量 `TDL_GUI_HOME` 使用独立数据目录。

## CLI

```powershell
# 环境检查
.\tdl-media.exe doctor

# 安装官方引擎
.\tdl-media.exe engine install --version v0.20.4

# 添加并扫码登录账户
.\tdl-media.exe account add "我的 Telegram"
.\tdl-media.exe account list --json
.\tdl-media.exe account login ACCOUNT_ID --method qr

# 获取聊天和媒体
.\tdl-media.exe chats refresh
.\tdl-media.exe chats list --json
.\tdl-media.exe media scan CHAT_ID --from 2026-01-01 --to 2026-09-14

# 创建规则、预览、下载
.\tdl-media.exe rules create --chat CHAT_ID --kinds photo,video --max-size 2147483648 --max-total-size 10737418240
.\tdl-media.exe rules list --json
.\tdl-media.exe media preview RULE_ID --json
.\tdl-media.exe jobs create PLAN_ID --json
.\tdl-media.exe jobs run JOB_ID
```

所有普通命令都支持 `--json`。GUI 使用 `tdl-media worker` 提供的逐行 JSON-RPC 2.0 协议，Telegram ID 始终作为字符串传输。

## 关键行为

- 预览会生成固定清单；任务恢复沿用原清单，重新运行规则才会重新筛选。
- 同一账户和聊天内按 Telegram 媒体 ID 去重。媒体重新上传并获得新 ID 时视为新文件。
- 已完成文件按记录和文件大小核对后跳过。未完成的单个文件恢复时重新下载。
- tdl 下载先进入任务暂存目录；程序逐项检查大小，再原子移动到最终路径。
- 默认路径模板为 `账号名/聊天名/yyyy-mm-dd-消息ID-原文件名`，会清理 Windows 非法名称并限制路径长度。
- 运行任务时关闭 GUI，可选择留在托盘继续运行，或保存进度后退出。

## 开发与构建

需要 Go 1.25+、Node.js 20+、Rust 1.84+ 和 Windows WebView2。运行：

```powershell
.\scripts\build.ps1 -Version 0.1.8
```

构建脚本运行 Go 测试、前端生产构建、Tauri 编译，并生成 `dist\TDL-Media-windows-x64-<version>.zip`。便携包不预装 tdl，首次使用时从官方发布页下载并校验。

## 许可

本项目与 AGPL-3.0 的 tdl 组件集成，因此按 AGPL-3.0 发布。
