# wxctl

Linux 桌面微信多开实例管理工具。通过为每个实例设置独立的 `HOME`，在同一用户下运行多个微信账号。

> 旧脚本 [scripts/wechat-profile.sh](scripts/wechat-profile.sh) 仅用于验证 Linux 机制，已被 `wxctl` 取代，请勿再手工维护。

## 安装

```bash
go install github.com/star-plan/wechatctl/cmd/wxctl@latest
# 或本地构建
go build -o wxctl ./cmd/wxctl
```

可再执行 `sudo install -m 755 wxctl /usr/local/bin/wxctl`。请确保 `wxctl` 在 `PATH` 中，以便桌面快捷方式能正确启动。

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

# 或从应用菜单点击对应快捷方式

# 查看运行状态 / 停止 / 重启
wxctl status
wxctl stop work
wxctl restart work --detach

# 修改显示名等
wxctl edit work --alias "外包项目号"

# 注销实例（默认保留聊天数据）
wxctl remove work

# 注销并删除数据（需确认）
wxctl remove work --purge
wxctl remove work --purge --yes
```

## 隔离方式：`linux-home`

为每个实例指定独立 `HOME`：

```text
实例 A → ~/.local/share/wxctl/instances/a
实例 B → ~/.local/share/wxctl/instances/b
```

前提：微信客户端本身已解除单实例限制。`wxctl` 不负责微信多开 Hook，只负责实例数据目录隔离。

## 数据布局

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

## 配置

```bash
wxctl config get
wxctl config set wechat_bin /usr/bin/wechat
wxctl config set shared_dir ~/Documents/WeChat-Shared
```

全局项：`wechat_bin`、`profiles_root`、`shared_dir`、`im_module`。单实例可通过 `wxctl edit` 覆盖 `wechat-bin` / `im-module`。

## 迁移与换机

若之前用过 `wechat-profile.sh`（仅 Linux）：

```bash
wxctl migrate
```

会将 `~/.local/share/wechat-profiles/*` 迁入 `~/.local/share/wxctl/instances/`，并生成注册表与 `.desktop` 文件。

换机时可导出/导入**元数据**（不含聊天记录）：

```bash
wxctl export ~/wxctl-meta.toml
# 新机器上：
wxctl import ~/wxctl-meta.toml
```

导入会创建对应的实例目录；聊天数据请自行备份 `~/.local/share/wxctl/instances/<name>`。导入时实例会统一使用 `linux-home` 后端。

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
| `desktop sync` | 同步桌面快捷方式 |
| `config get\|set` | 全局配置 |
| `migrate` | 从旧 Linux profiles 迁移 |
| `export` / `import` | 元数据导入导出 |

## 说明

- 实例名仅允许 `[a-zA-Z0-9_-]+`；中文显示名请用 `--alias`
- 启动时注入 `HOME`、`WXCTL_INSTANCE` 以及 Fcitx 相关输入法环境变量
- 不设置缩放相关环境变量
- 第一版不探测微信号，请用 alias/note 自行标注账号用途
