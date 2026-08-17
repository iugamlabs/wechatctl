# wechatctl Windows 多用户隔离方案

## 目标

在 Windows 下为每个微信实例提供独立的用户环境，从而实现：

* 多个微信账号同时运行
* 每个实例拥有独立登录态
* 重启微信后无需重新扫码
* 各实例配置、缓存、聊天数据互不干扰

前提：当前微信客户端已经通过现有 Hook 方案解除单实例限制，`wechatctl` 不负责处理微信多开本身，只负责实例隔离。

## 核心思路

Linux 版本通过为每个微信实例指定独立 `HOME`：

```text
实例 A → ~/.wechatctl/a
实例 B → ~/.wechatctl/b
实例 C → ~/.wechatctl/c
```

Windows 下则使用独立的本地 Windows 用户：

```text
主微信      → 当前用户 deali
工作微信    → wechatctl_work
测试微信    → wechatctl_test
```

每个 Windows 用户天然拥有独立的：

```text
%USERPROFILE%
%APPDATA%
%LOCALAPPDATA%
Documents
HKEY_CURRENT_USER
DPAPI 用户密钥
```

因此微信的登录状态、配置、缓存和聊天数据可以天然隔离。

## 启动方式

`wechatctl` 使用 Windows API：

```text
CreateProcessWithLogonW
```

并指定：

```text
LOGON_WITH_PROFILE
```

以对应的本地用户身份启动同一个 `Weixin.exe`。

例如：

```text
wechatctl start work
```

内部流程：

```text
读取实例配置
    ↓
解密 wechatctl_work 用户密码
    ↓
CreateProcessWithLogonW
    ↓
加载 wechatctl_work User Profile
    ↓
启动 Weixin.exe
```

微信窗口仍然显示在当前 Windows 桌面，不需要切换 Windows 用户。

## 实例创建

例如：

```powershell
wechatctl create work
```

内部执行：

1. 创建普通本地用户：

```text
wechatctl_work
```

2. 自动生成随机密码。

3. 初始化 Windows User Profile。

4. 使用当前用户的 DPAPI 加密密码并保存。

5. 创建对应的 `wechatctl` 实例配置。

配置示例：

```json
{
  "name": "work",
  "backend": "windows-user",
  "username": "wechatctl_work",
  "encrypted_password": "...",
  "wechat_path": "C:\\Program Files\\Tencent\\Weixin\\Weixin.exe"
}
```

## 命令设计

尽量与 Linux 版本保持一致：

```powershell
wechatctl create work
wechatctl start work
wechatctl stop work
wechatctl restart work
wechatctl list
wechatctl show work
wechatctl remove work
```

例如：

```text
NAME       BACKEND        USER              STATUS
work       windows-user   wechatctl_work    running
test       windows-user   wechatctl_test    stopped
```

## 文件共享

不同 Windows 用户默认无法访问彼此的部分私人目录。

可以统一建立共享目录：

```text
D:\WechatShare
├── work
└── test
```

或者：

```text
C:\Users\Public\wechatctl-share
```

`wechatctl` 创建实例时自动配置 ACL，让主用户和对应微信用户都拥有读写权限。

## 微信安装

所有实例共享同一份微信程序：

```text
C:\Program Files\Tencent\Weixin\Weixin.exe
```

不要为每个实例复制一套微信。

微信升级由主 Windows 用户完成，其他实例重新启动后直接使用新版本。

## 架构建议

将现有 `wechatctl` 抽象成不同平台 Backend：

```text
Linux
└── LinuxHomeBackend

Windows
└── WindowsUserBackend
```

例如：

```go
type InstanceBackend interface {
    Create(name string) error
    Start(name string) error
    Stop(name string) error
    Remove(name string) error
    Status(name string) (*InstanceStatus, error)
}
```

这样 CLI 和实例管理逻辑可以跨平台复用，仅底层隔离实现不同。

## Sandboxie

Sandboxie 也可以实现类似的数据隔离，但目前不是首选。

原因：

* 增加第三方驱动和服务依赖
* 微信升级可能产生兼容性问题
* 文件、注册表、IPC 都经过虚拟化层
* 当前已经通过 Hook 解决微信单实例限制，Sandboxie 的 IPC 隔离优势意义不大

因此建议：

```text
WindowsUserBackend → 正式稳定方案
SandboxieBackend   → 后续可选方案
```

## 最小验证步骤

正式开发前先验证核心链路：

```text
创建 wechatctl_test 本地用户
        ↓
CreateProcessWithLogonW 启动微信
        ↓
扫码登录
        ↓
退出微信
        ↓
再次以同一用户启动
        ↓
检查登录态是否保留
        ↓
同时启动多个不同用户的微信实例
```

如果上述流程正常，Windows 版 `wechatctl` 的核心技术路线即可确定。
