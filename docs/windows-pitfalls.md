# Windows 多用户隔离踩坑记录

实现 `windows-user` 后端（独立本地用户 + `CreateProcessWithLogonW`）时踩过的坑。方案背景见 [changes/windows-multi-users.md](changes/windows-multi-users.md)。

`wxctl` 只负责实例隔离，不负责解除微信单实例限制。

## 1. Weixin.exe 只是启动器

安装目录实际是：

```text
C:\Program Files\Tencent\Weixin\
  Weixin.exe          # 约 3MB 启动器
  4.1.7.33\
    Weixin.dll        # 真正的程序（约 175MB）及一堆原生模块
```

官方快捷方式的工作目录是 `C:\Program Files\Tencent\Weixin`。启动器会 `SetCurrentDirectory("4.x.x.x")`，再 delay-load `Weixin.dll`。

用户数据在当前用户下：

```text
%APPDATA%\Tencent\xwechat\
%USERPROFILE%\Documents\xwechat_files\
HKCU\Software\Tencent\Weixin   # InstallPath、Version
```

不要把 CWD 设成 `C:\Users\wechatctl_<name>`。应设为安装目录，并补上 `--user-lib-dir=<版本目录>`。

## 2. `0xc06d007e`：delay-load 找不到 DLL

含义是 `VcppException(ERROR_SEVERITY_ERROR, ERROR_MOD_NOT_FOUND)`。

常见原因：

* CWD 不是安装目录，启动器去用户 Profile 下找 `4.x.x.x\Weixin.dll`
* 隔离用户不在 `Users` 组，读不了 `Program Files`

`CreateProcessWithLogonW` 打开主 EXE 用的是**调用者**的权限，所以 `Weixin.exe` 能起来；随后加载 DLL 用的是**目标用户**令牌。主程序弹出来再报模块找不到，就是这个时序。

`NetUserAdd` 建出来的本地用户默认可以不属于任何组（`net user` 显示本地组成员 `*None`）。创建后必须显式加入 `Users`（组名用 `WinBuiltinUsersSid` 解析，中文系统显示为「用户」）。

## 3. `0xc0000142`：不要用 cmd.exe 初始化 Profile

`create` 早期用 `cmd.exe /c exit 0` 触发首次登录、创建 `C:\Users\wechatctl_*`。`cmd.exe` 会连当前交互桌面，隔离用户没有 WinSta0/Default 权限时，`user32` 初始化失败：

```text
应用程序无法正常启动 (0xc0000142)
```

正确做法是 `LogonUser` + `LoadUserProfile`（需启用 `SeRestorePrivilege` / `SeBackupPrivilege`），不要为了建 Profile 再拉一个 GUI/控制台进程。

往隔离用户写注册表同样不要 `reg.exe`：在 hive 已加载时直接写 `HKU\<SID>\...`。

## 4. 必须授权当前窗口站和桌面

以另一用户在**当前桌面**显示窗口，需要给该用户 ACE：

* 窗口站 `WinSta0`：`WINSTA_ALL_ACCESS`（含子对象继承）
* 桌面 `Default`：`DESKTOP_ALL_ACCESS`

用 `GetProcessWindowStation` / `OpenDesktop` + `GetSecurityInfo` / `SetSecurityInfo`（`SE_WINDOW_OBJECT`）。这是当前交互用户对自己桌面的授权，`start` 不必提权。

缺少授权时：`create` 里的辅助进程报 `0xc0000142`，微信则可能无窗口或立刻退出。

## 5. 微信 4 是 Chromium：不要 Job，不要 SUSPENDED

`CREATE_SUSPENDED` + `AssignProcessToJobObject` 再 `ResumeThread`，微信会**静默退出**（无对话框）。隔离用户的 `%APPDATA%\Tencent\xwechat` 也不会出现。

官方子进程命令行类似：

```text
Weixin.exe --user-lib-dir="C:\Program Files\Tencent\Weixin\4.1.7.33" --no-sandbox --type=...
```

启动主进程时也不要 suspend、不要放进 Job。进程跟踪用 pid 文件；`stop` 先试 `TerminateProcess`，失败再 `taskkill /T /F`。

## 6. `--detach` 会把立刻退出当成成功

`CreateProcessWithLogonW` 成功只表示进程创建成功。单实例互斥或 Chromium 启动失败时，进程马上结束，`--detach` 仍打印 started。

`--detach` 应等待约 2 秒，用 `GetExitCodeProcess` 确认仍是 `STILL_ACTIVE`，否则返回明确错误。

单实例检查往往是会话级 / `Global\` 互斥体，**换 Windows 用户也挡不住**。本机已有微信窗口时，第二个 `Weixin.exe` 可能直接退出。`wxctl` 不负责 Hook；若立刻退出，先关掉已有微信再试，或确认 Hook 对「其他 Windows 用户」也生效。

## 7. 灰色边框：事后改窗口样式治不好

隔离用户没有走 winlogon/userinit，视觉样式 / DWM 非客户区渲染失败。微信 4 自己画标题栏，外面再被系统套一层经典灰框。

只抄 `ThemeManager` 注册表不够。隔离用户进程用不上**当前交互登录会话**的主题，这是 Windows 对 RunAs / `CreateProcessWithLogonW` 的长期限制。

实测过两套「窗口出现后再改样式」的办法，**都没能稳定去掉灰框**：

1. **启动后短时轮询 `EnumWindows`**  
   去掉 `WS_CAPTION` / `WS_DLGFRAME`、清 `WS_EX_*EDGE`，并设  
   `DWMWA_NCRENDERING_POLICY` / `DWMWA_BORDER_COLOR` / `DWMWA_WINDOW_CORNER_PREFERENCE`。  
   有一次看起来对**登录窗**生效：先有灰框，等一会消失。点登录进主界面后灰框又回来。  
   原因：登录窗和主界面是**不同 HWND**，登录成功后主窗口才创建，几秒轮询早已结束。

2. **进程外 `SetWinEventHook`（`WINEVENT_OUTOFCONTEXT`）**  
   `start` 拉起隐藏的 `wxctl _watch-frame <pid>`，监听 `EVENT_OBJECT_CREATE` / `SHOW`，对进程树里的大窗口立刻修边框，微信退出后 watcher 结束。  
   这比轮询覆盖得全，也不是注入微信进程。但实测**登录窗和主界面仍然都有灰色边框**。

结论：

* 灰框来自系统给「另一用户 token」画的经典非客户区，不是漏修了某个 HWND。
* 微信 / Chromium 可能在创建后自己再改窗口样式，外部 `SetWindowLong` / `DwmSetWindowAttribute` 会被盖掉，或 DWM 根本不给该 token 做现代非客户区。
* 靠外部改 HWND **不能作为可靠方案**。要根治需要进程内配合（例如注入 `uxtheme`、改隔离后端），或接受灰框。当前 `windows-user` 后端把这视为已知限制。

## 8. 输入法：隔离用户没有可用的 TSF

IME 挂在**当前登录用户的会话服务**上，不是随任意进程 token 自动出现。另一用户的 GUI 默认不能切中文。

试过：

1. 把当前用户的 `Software\Microsoft\CTF`、`Keyboard Layout`、`Control Panel\International`、主题和 DPI 键写进隔离用户 hive
2. 以隔离用户、绑定 `winsta0\default` 启动 `System32\ctfmon.exe`

实测**仍无法使用输入法**（登录窗、主界面都一样）。

Windows 11 的 `TextInputHost` 属于当前登录用户，跨用户调用本来就不完整。抄注册表 + 拉 `ctfmon` 不足以把 IME 接到另一用户的微信进程上。

第三方输入法若安装为「所有用户」也许略好，但未作为可靠方案验证。当前视为 `windows-user` 的已知限制：能隔离登录态，交互体验（主题边框、输入法）弱于本机用户直接打开的微信。

## 9. 其它注意点

| 点 | 说明 |
|----|------|
| 提权 | `create` / `remove --purge` 需要管理员；`start` / `stop` 一般不需要 |
| DPAPI | 密码用当前用户 DPAPI 加密，换 Windows 用户或换机无法解密，导出元数据时丢弃密文 |
| SAM 名 | 本地用户名最长 20 字符；`wechatctl_` + 长实例名会超限，需缩短并加哈希 |
| 不要用 `CreateProcessAsUser` | 需要 `SeAssignPrimaryToken`，普通管理员没有；用 `CreateProcessWithLogonW` |
| `LoadUserProfile` | 自定义环境块前应先加载 Profile，否则 `APPDATA` / `USERPROFILE` 可能不完整 |
| 共享目录 | 用 `%PUBLIC%\wechatctl-share\<name>`，给主用户和实例用户都加可继承修改 ACE |
| 同一份微信 | 不要为每个实例复制 `Weixin.exe`；升级由主用户完成 |

## 10. 建议的最小验证

```powershell
# 管理员
wxctl create work
net localgroup Users wechatctl_work /add   # 若 create 已加过会提示已是成员

# 可先退出本机已打开的微信，排除单实例 Hook 未覆盖跨用户的情况
wxctl start work --detach
wxctl list
```

确认：弹出登录窗、无 `0xc06d007e` / `0xc0000142`、退出后再 `start` 登录态还在。

已知未解决：灰色系统边框、隔离用户进程内无法使用输入法（见第 7、8 节）。
