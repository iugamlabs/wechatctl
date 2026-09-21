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
| `init-data` | 从运行中的微信进程提取 SQLCipher 密钥（需 root/CAP_SYS_PTRACE） |
| `sessions` / `unread` | 会话列表 / 未读会话 |
| `history` / `search` | 聊天记录 / 全文搜索 |
| `contacts` / `members` | 联系人 / 群成员 |
| `chat-export` | 导出聊天记录为 Markdown 或纯文本 |
| `new-messages` | 自上次检查以来的新消息 |

## 聊天数据查询（Linux）

在实例生命周期之外，`wxctl` 可读取该实例微信加密库中的会话、联系人、消息等。**查询不需要 sudo，也不要求微信正在运行**；只要实例曾登录且已执行过 `init-data`，在 `wxctl stop` 之后仍可离线查询。

### 首次准备：init-data

抽密钥时**必须**先启动对应实例的微信，并使用 root 或 `CAP_SYS_PTRACE`（Ubuntu 等默认 `kernel.yama.ptrace_scope=1`，同用户 ptrace 通常失败，因 wxctl 用 `Setsid` 拉起微信而非父子进程）：

```bash
wxctl start work --detach
sudo wxctl init-data work
wxctl stop work    # 之后仍可 wxctl sessions work 等
```

`sudo wxctl ...` 会通过 `SUDO_USER` 解析你的真实 HOME（也可用 `WXCTL_REAL_HOME` 覆盖），无需 `sudo -E`。若需 capability 而非每次 sudo，可将已安装到受控路径（如 `/usr/local/bin`）的二进制执行 `sudo setcap cap_sys_ptrace=ep $(command -v wxctl)`——**不要**对 world-writable 路径 setcap。

`init-data` 默认输出 text；可用 `--format json`。密钥与 wxid 轮换后：`wxctl init-data --force work`。

### 数据与缓存路径

每个实例的状态在伪造 HOME **之外**：

```text
~/.local/share/wxctl/wxdata/<name>/
  all_keys.json       # SQLCipher 密钥（0600），等同于账号敏感数据
  last_check.json     # new-messages 游标
  lock                # 解密与游标写入共用 flock
  cache/              # 解密后的明文 sqlite（0600），目录 0700
    _mtimes.json
    <hash>.db
```

`wxctl remove work --purge` 会同时删除 `instances/<name>` 与上述 `wxdata/<name>`。

v1 **没有** `cache-clear` 子命令。需要强制全量重解密时：

```bash
rm -rf ~/.local/share/wxctl/wxdata/<name>/cache
```

解密前会把加密库（及存在的 `-wal`）**短暂复制为快照**再解密，以缩小与微信并发写 WAL 的撕裂窗口；仍可能在极端情况下遇到坏帧，此时删除 `cache/` 后重试。可选调试：`WXCTL_DEBUG=1` 时在 stderr 打印解密进度。

### 常用查询示例

```bash
wxctl sessions work --limit 10 --format json
wxctl unread work --format json
wxctl contacts work --query 张三 --limit 20
wxctl history work '文件传输助手' --limit 20 --format text
wxctl search work 关键词 --limit 50
wxctl members work '某群名称'
wxctl chat-export work '某聊天' -o chat.md --format markdown
wxctl new-messages work --format json
```

查询类命令 `--format` 默认为 `json`（`init-data` 默认为 text；`chat-export` 为 `markdown|txt`）。

**已知限制：** `search` 使用 SQLite `LIKE`；会话摘要等经 zstd 压缩的 blob **不会**被 LIKE 命中（与 wechat-cli 相同）。

元数据换机仍用 **`wxctl export` / `wxctl import`**（不含聊天内容）；聊天记录请自行备份 `~/.local/share/wxctl/instances/<name>` 与/或 `wxdata/<name>`。

### Live 集成测试

在已对本机实例执行 `init-data` 后：

```bash
WXCTL_LIVE_INSTANCE=work go test ./internal/wxdata/... -count=1 -timeout 120s
```

未设置 `WXCTL_LIVE_INSTANCE` 时该测试自动 Skip，`go test ./...` 在 CI/无微信环境下保持全绿。Live 测试不断言具体聊天正文（隐私）。

## 说明

- 实例名仅允许 `[a-zA-Z0-9_-]+`；中文显示名请用 `--alias`
- 启动时注入 `HOME`、`WXCTL_INSTANCE` 以及 Fcitx 相关输入法环境变量
- 不设置缩放相关环境变量
- 第一版不探测微信号，请用 alias/note 自行标注账号用途
