# wxctl

微信多开实例管理工具。Linux 通过独立 `HOME` 隔离；Windows 保持当前登录用户不变，仅为每个微信实例重定向独立的 AppData 数据目录，从而同时运行多个账号并保留各自登录态。

> 旧脚本 [scripts/wechat-profile.sh](scripts/wechat-profile.sh) 仅用于验证 Linux 机制，已被 `wxctl` 取代，请勿再手工维护。

## 安装

```bash
go install github.com/star-plan/wechatctl/cmd/wxctl@latest
# 或本地构建
go build -o wxctl ./cmd/wxctl          # Linux
go build -o wxctl.exe ./cmd/wxctl      # Windows
```

Linux 下可再执行 `sudo install -m 755 wxctl /usr/local/bin/wxctl`。请确保 `wxctl` 在 `PATH` 中，以便桌面 / 开始菜单快捷方式能正确启动。

Windows 的默认 `windows-redirect` 后端不需要管理员权限；`create`、`start`、`stop` 和 `remove --purge` 均在当前登录用户下运行。

## 快速开始

```bash
# 创建实例（alias 可用中文）
wxctl create work --alias "公司号" --note "工作账号"

# 查看列表 / 详情
wxctl list
wxctl show work

# 前台启动（挂在当前终端）
wxctl start work

# 后台启动
wxctl start work --detach

# 或从应用菜单 / 开始菜单点击对应快捷方式

# 查看运行状态 / 停止 / 重启
wxctl status
wxctl stop work
wxctl restart work --detach

# 修改显示名等
wxctl edit work --alias "外包项目号"

# 注销实例（默认保留聊天数据）
wxctl remove work

# 注销并删除数据（需确认；Windows 还会删除对应本地用户）
wxctl remove work --purge
wxctl remove work --purge --yes
```

## 平台隔离方式

### Linux：`linux-home`

为每个实例指定独立 `HOME`：

```text
实例 A → ~/.local/share/wxctl/instances/a
实例 B → ~/.local/share/wxctl/instances/b
```

### Windows：`windows-redirect`

每个实例仍由**当前 Windows 登录用户**通过普通进程创建启动。wxctl 复制当前环境变量，仅覆盖 `APPDATA`、`LOCALAPPDATA` 和 `WXCTL_INSTANCE`，并将工作目录设为微信安装目录；对新版 `Weixin.exe` 自动传入对应的 `--user-lib-dir`。

```text
wxctl create work
    → %LOCALAPPDATA%\wxctl\instances\work\AppData\Roaming
    → %LOCALAPPDATA%\wxctl\instances\work\AppData\Local
    → %LOCALAPPDATA%\wxctl\instances\work\Documents（预建，供验证）
```

不会创建本地用户，不会加载其他 Windows Profile，也不会隔离或修改注册表。`USERPROFILE`、`HOMEDRIVE` 和 `HOMEPATH` 保持当前用户值，以避免影响 DWM、主题、TSF/中文输入法、剪贴板、拖拽和托盘等桌面能力。

旧 `windows-user` 后端已移除。保留该后端标识的历史实例不能再启动；请在确认聊天数据备份后，用 `wxctl create <name>` 创建新的 `windows-redirect` 实例。wxctl 不会自动删除旧本地用户或其 Profile。

前提：微信客户端本身已解除单实例限制。`wxctl` 不负责微信多开 Hook，只负责实例数据目录隔离。

## 数据布局

### Linux

```text
~/.config/wxctl/
  config.toml
  instances.toml

~/.local/share/wxctl/
  instances/<name>/     # 该实例伪造 HOME（聊天记录等）
  run/<name>.pid

~/.local/share/applications/
  wxctl-<name>.desktop

~/Documents/WeChat-Shared/   # 各实例共享目录（实例内 Shared 软链）
```

### Windows

```text
%APPDATA%\wxctl\
  config.toml
  instances.toml

%LOCALAPPDATA%\wxctl\
  instances\<name>\
    AppData\Roaming\          # 该实例的 %APPDATA%
    AppData\Local\            # 该实例的 %LOCALAPPDATA%
    Documents\                 # 已创建；是否被微信使用需实测
  run\<name>.pid

%APPDATA%\Microsoft\Windows\Start Menu\Programs\wxctl\
  wxctl-<name>.lnk

%PUBLIC%\wechatctl-share\     # 项目已有的可选共享目录
```

所有实例共享同一份微信程序，例如 `C:\Program Files\Tencent\Weixin\Weixin.exe`，不要为每个实例复制一套微信。

## 配置

```bash
wxctl config get
wxctl config set wechat_bin "C:\Program Files\Tencent\Weixin\Weixin.exe"
wxctl config set shared_dir "C:\Users\Public\wechatctl-share"
```

全局项：`wechat_bin`、`profiles_root`、`shared_dir`、`im_module`。单实例可通过 `wxctl edit` 覆盖 `wechat-bin` / `im-module`。

Windows 启动时会自动探测常见微信安装路径；找不到时请用 `config set wechat_bin` 指定。

## 迁移与换机

若之前用过 `wechat-profile.sh`（仅 Linux）：

```bash
wxctl migrate
```

会将 `~/.local/share/wechat-profiles/*` 迁入 `~/.local/share/wxctl/instances/`，并生成注册表与 `.desktop`。

换机时导出/导入**元数据**（不含聊天记录，也不含 Windows DPAPI 密码）：

```bash
wxctl export ~/wxctl-meta.toml
# 新机器上：
wxctl import ~/wxctl-meta.toml
```

Windows 导入会创建对应的实例目录；聊天数据请自行备份 `%LOCALAPPDATA%\wxctl\instances\<name>`。导入时实例统一转换为当前平台的主后端。

重建全部菜单入口：

```bash
wxctl desktop sync
```

## 命令一览

| 命令 | 说明 |
|------|------|
| `create` | 创建实例与隔离环境 |
| `list` / `ls` | 列出实例、后端、用户与运行状态 |
| `show` | 实例详情 |
| `start` | 启动微信（`--detach` 后台） |
| `stop` | 停止实例 |
| `restart` | 重启实例 |
| `status` | 全部运行状态 |
| `edit` | 改 alias/tags/note 等 |
| `remove` / `rm` | 注销（默认不删数据） |
| `desktop sync` | 同步桌面 / 开始菜单快捷方式 |
| `config get\|set` | 全局配置 |
| `migrate` | 从旧 Linux profiles 迁移 |
| `export` / `import` | 元数据导入导出 |

## 说明

- 实例名仅允许 `[a-zA-Z0-9_-]+`；中文显示名请用 `--alias`
- Linux 启动时注入 `HOME`、`WXCTL_INSTANCE` 以及 Fcitx 相关输入法环境变量
- Windows 当前阶段只验证 `APPDATA` / `LOCALAPPDATA` 重定向。若实测发现 `xwechat_files` 仍写入真实 Documents，才会评估一个仅重定向 RoamingAppData、LocalAppData、Documents 的 Known Folder Hook；不会实现通用文件系统或注册表虚拟化。
- 不设置缩放相关环境变量
- 第一版不探测微信号，请用 alias/note 自行标注账号用途
- Sandboxie 不是当前方案；隔离依赖当前用户进程环境，而不是第三方沙箱
