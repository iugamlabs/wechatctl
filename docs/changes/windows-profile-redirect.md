你正在维护 Go 项目 **wxctl / wechatctl**。现有 Linux 实现通过给每个微信实例设置独立 `HOME`，实现多个微信账号的数据和登录态隔离。

Windows 之前实现过 `windows-user` 后端：为每个实例创建独立 Windows 用户，再通过 `CreateProcessWithLogonW` 启动微信。该方案虽然能够隔离数据，但存在跨用户 GUI 的系统级问题，包括 DWM/主题异常、灰色边框以及 TSF/中文输入法不可用，因此不再作为主方案。

现在实现一个新的 Windows 隔离方案。

## 核心目标

**所有微信实例仍然使用当前登录的 Windows 用户运行，只隔离微信的数据目录。**

这样应保持：

* 正常 DWM / Windows 主题
* 正常中文输入法 / TSF
* 正常剪贴板、拖拽、托盘等桌面能力
* 每个微信实例拥有独立登录态和用户数据

不要创建额外 Windows 用户。

不要隔离注册表。

不要引入 Sandboxie、MSIX 或其他外部运行时依赖。

## 已知微信结构

当前微信安装结构类似：

```text
C:\Program Files\Tencent\Weixin\
  Weixin.exe
  4.1.7.33\
    Weixin.dll
```

`Weixin.exe` 是启动器。

启动时需要：

* CWD 设置为微信安装目录
* 必要时传递：

```text
--user-lib-dir=<微信版本目录>
```

已知主要用户数据位于：

```text
%APPDATA%\Tencent\xwechat\
%USERPROFILE%\Documents\xwechat_files\
```

注册表目前不做任何隔离。

## 第一阶段：优先实现纯环境变量方案

先不要写 DLL Hook。

为每个实例创建独立目录，例如：

```text
%LOCALAPPDATA%\wxctl\instances\work\
  AppData\
    Roaming\
    Local\
  Documents\
```

启动实例时复制当前进程环境变量，只覆盖必要项，例如：

```text
APPDATA=<instance>\AppData\Roaming
LOCALAPPDATA=<instance>\AppData\Local
```

如果合理，也可以实验性调整：

```text
USERPROFILE
HOMEDRIVE
HOMEPATH
```

但必须注意不要为了模拟完整 Windows Profile 而破坏当前用户的桌面环境。

微信进程必须仍然使用**当前 Windows 用户 Token**，直接通过普通 `CreateProcess` / Go `exec.Cmd` 等方式启动。

不要再使用：

```text
CreateProcessWithLogonW
LogonUser
LoadUserProfile
Windows 本地用户
WinSta0 ACL
Desktop ACL
```

## Documents 隔离

需要特别确认：

```text
%USERPROFILE%\Documents\xwechat_files
```

微信到底通过：

1. 环境变量拼路径；
2. `SHGetKnownFolderPath(FOLDERID_Documents)`；
3. 其他 Windows API；

中的哪种方式获取。

不要一开始过度设计。

先运行 PoC，通过实际文件生成位置判断纯环境变量是否足够。

## 第二阶段：仅在环境变量不足时增加轻量 Hook

如果实测发现微信通过：

```text
SHGetKnownFolderPath
SHGetFolderPath
```

获取 AppData / Documents，导致环境变量无法完成隔离，再实现一个极小的 Windows runtime DLL。

只 Hook 必要的 Known Folder 查询。

例如：

```text
FOLDERID_RoamingAppData
    → <instance>\AppData\Roaming

FOLDERID_LocalAppData
    → <instance>\AppData\Local

FOLDERID_Documents
    → <instance>\Documents
```

其他 Known Folder 必须保持系统原始行为。

不要做通用文件系统 Hook。

不要 Hook：

```text
CreateFile
NtCreateFile
Registry API
```

除非后续有明确证据证明必须这么做。

目标不是实现“小型 Sandboxie”，而是实现**最小范围的微信 Profile Path Redirect**。

## 注册表

本方案明确：

**完全不隔离注册表。**

不要实现：

```text
RegOverridePredefKey
RegLoadAppKey
HKCU virtualization
registry.dat
```

微信安装路径、版本等注册表数据由所有实例共享。

只有未来发现明确的账号状态实际存储于注册表，并且造成实例冲突时，再单独评估。

## 与现有多开机制的关系

wxctl 不负责解除微信单实例限制。

当前机器上的微信已经通过已有 Hook 方案解除单实例限制，可以同时启动任意多个 `Weixin.exe`。

新的 Windows backend 只负责：

```text
实例 A → 数据目录 A
实例 B → 数据目录 B
实例 C → 数据目录 C
```

不要重新实现 Mutex / singleton bypass。

## 建议 Backend

可以新增：

```text
WindowsRedirectBackend
```

整体结构：

```text
Linux
└── HomeBackend

Windows
├── WindowsRedirectBackend   # 新主方案
└── WindowsUserBackend       # 保留，标记 experimental
```

尽量复用现有：

```text
create
start
stop
list
show
remove
```

等命令和实例模型。

## 实施顺序

严格按照“小步验证”方式开发。

### Step 1

实现最小 PoC：

```text
当前 Windows 用户
+
独立 APPDATA
+
独立 LOCALAPPDATA
+
正确 CWD
+
--user-lib-dir
```

启动两个微信实例。

观察实际生成的数据目录。

### Step 2

验证两个实例：

* 可以同时运行
* 可以分别扫码两个账号
* 登录状态互不覆盖
* 退出后重新启动仍保持各自登录状态
* 中文输入法正常
* Windows 主题 / DWM 正常
* 聊天数据互不混淆

### Step 3

如果 `Documents\xwechat_files` 仍然落到真实用户 Documents：

分析微信获取 Documents 路径的方法。

只有确认环境变量不足后，才实现 Known Folder Hook。

### Step 4

如果需要 DLL Hook，将其控制在最小范围，只重定向：

```text
RoamingAppData
LocalAppData
Documents
```

不要扩展成完整沙箱。

## 实例目录建议

最终实例数据尽量集中：

```text
%LOCALAPPDATA%\wxctl\instances\
  work\
    AppData\
      Roaming\
      Local\
    Documents\
    instance.json

  personal\
    AppData\
      Roaming\
      Local\
    Documents\
    instance.json
```

如果当前项目已有实例目录规范，则优先复用现有结构，不要为了这个方案做无必要的大规模重构。

## 验收标准

最重要的验收场景：

```text
wxctl create work
wxctl create test

wxctl start work
wxctl start test
```

分别登录两个不同微信账号。

关闭所有微信后：

```text
wxctl start work
wxctl start test
```

应满足：

1. 两个实例仍然分别保持原来的登录账号；
2. 不需要重新扫码；
3. 数据互不覆盖；
4. 中文输入法正常；
5. 微信窗口样式与普通启动完全一致；
6. 两个进程都属于当前 Windows 登录用户；
7. 不创建额外 Windows 用户；
8. 不依赖 Sandboxie 等第三方软件；
9. 不修改或隔离注册表。

## 开发原则

优先选择最简单、最少侵入的实现。

顺序必须是：

```text
环境变量
    ↓ 不够
Known Folder 定向 Hook
    ↓
到此为止
```

不要提前实现完整文件系统虚拟化。

不要为了理论完整性增加目前没有实际需求的机制。

请先阅读现有 Windows backend、实例模型、CLI 和微信启动代码，在保持现有项目风格的基础上完成实现，并在完成后给出：

* 修改文件列表
* 核心实现说明
* 实际验证结果
* 环境变量方案是否足够
* 是否最终需要 Known Folder Hook
* 仍存在的限制或风险
