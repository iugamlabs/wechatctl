//go:build windows

package backend

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/paths"
	"golang.org/x/sys/windows"
)

// windowsRedirect 是 Windows 的主隔离后端。它不切换 Windows 用户、
// 不加载其他 Profile，也不触碰注册表；微信继承当前登录用户 Token，
// 仅通过进程环境变量获得各实例独立的 AppData 路径。
type windowsRedirect struct {
	layout paths.Layout
	cfg    config.Config
}

func (b windowsRedirect) Name() string { return BackendWindowsRedirect }

func (b windowsRedirect) resolved() config.Config {
	return b.cfg.Resolve(b.layout)
}

// Create 准备微信可写的最小目录集。Documents 预先建立，供后续实测判断
// 微信是否能仅靠环境变量使用它；第一阶段刻意不伪造 USERPROFILE。
func (b windowsRedirect) Create(inst config.Instance) (CreateResult, error) {
	if inst.Backend != "" && inst.Backend != BackendWindowsRedirect {
		return CreateResult{}, fmt.Errorf("cannot create instance %q with backend %s on the redirect backend", inst.Name, inst.Backend)
	}
	for _, dir := range []string{
		b.HomeDir(inst),
		b.roamingDir(inst),
		b.localDir(inst),
		b.documentsDir(inst),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return CreateResult{}, err
		}
	}
	return CreateResult{Backend: BackendWindowsRedirect}, nil
}

// Start 使用 Go 的普通 CreateProcess 路径，因此子进程继承当前登录用户的
// Windows Token，保留 DWM、TSF、剪贴板、拖拽和托盘等当前桌面能力。
func (b windowsRedirect) Start(inst config.Instance, opts StartOptions) error {
	if inst.Backend != "" && inst.Backend != BackendWindowsRedirect {
		return fmt.Errorf("instance %q uses removed backend %s; recreate it with wxctl create", inst.Name, inst.Backend)
	}
	if _, err := b.Create(config.Instance{Name: inst.Name}); err != nil {
		return err
	}
	if err := os.MkdirAll(b.layout.RunDir, 0o755); err != nil {
		return err
	}

	cfg := b.resolved()
	bin := inst.EffectiveWechatBin(cfg)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("wechat binary not found: %s (set with: wxctl config set wechat_bin <path>)", bin)
	}

	// Weixin.exe 是启动器，必须从安装目录运行；版本目录让启动器能够找到
	// 对应 Weixin.dll。该参数已有用户显式传入时不覆盖它。
	installDir, versionDir := weixinRuntimeDirs(bin)
	args := append([]string{}, opts.ExtraArgs...)
	if versionDir != "" && !hasUserLibDir(args) {
		args = append(args, "--user-lib-dir="+versionDir)
	}

	cmd := exec.Command(bin, args...)
	cmd.Dir = installDir
	cmd.Env = b.environment(inst)
	if opts.Detach {
		cmd.Stdin = nil
		cmd.Stdout = nil
		cmd.Stderr = nil
		// 不设置 HideWindow：该标志会影响子进程的主窗口显示。
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start wechat as current Windows user: %w", err)
	}
	pid := cmd.Process.Pid
	if err := writePid(b.layout.PidFile(inst.Name), pid); err != nil {
		_ = cmd.Process.Kill()
		return err
	}

	if opts.Detach {
		if err := waitCurrentProcessAlive(pid); err != nil {
			_ = cmd.Process.Release()
			_ = os.Remove(b.layout.PidFile(inst.Name))
			return err
		}
		return cmd.Process.Release()
	}
	defer func() { _ = os.Remove(b.layout.PidFile(inst.Name)) }()
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &ExitError{Code: exitErr.ExitCode()}
		}
		return err
	}
	return nil
}

// Stop 先正常关闭该实例进程树中的窗口；超时后仅结束该树。
func (b windowsRedirect) Stop(name string, timeout time.Duration) error {
	st := b.Status(name)
	if !st.Running {
		return fmt.Errorf("instance %q is not running", name)
	}
	// 启动器与主界面通常是同一 PID；超时后的 taskkill /T 会覆盖其子进程。
	requestClose([]uint32{uint32(st.PID)})
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !b.Status(name).Running {
			_ = os.Remove(b.layout.PidFile(name))
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := terminateProcessTree(st.PID); err != nil {
		return fmt.Errorf("stop instance %q: %w", name, err)
	}
	_ = os.Remove(b.layout.PidFile(name))
	return nil
}

// Remove 只删除 wxctl 管理的实例目录；不会删除 Windows 用户、Profile 或注册表。
func (b windowsRedirect) Remove(inst config.Instance, purge bool) error {
	if !purge {
		return nil
	}
	home := b.HomeDir(inst)
	if _, err := os.Stat(home); os.IsNotExist(err) {
		return nil
	}
	return os.RemoveAll(home)
}

// Status 通过 wxctl 写入的启动器 PID 判断状态。微信的多开机制由外部既有
// Hook 负责，因此本后端不创建或绕过任何 singleton / mutex。
func (b windowsRedirect) Status(name string) Status {
	st := Status{Name: name}
	pid, err := readPid(b.layout.PidFile(name))
	if err != nil {
		if !os.IsNotExist(err) {
			st.Error = err.Error()
		}
		return st
	}
	if !processAlive(pid) {
		_ = os.Remove(b.layout.PidFile(name))
		return st
	}
	st.Running = true
	st.PID = pid
	return st
}

// HomeDir 返回集中存放实例数据的根目录。
func (b windowsRedirect) HomeDir(inst config.Instance) string {
	return filepath.Join(b.resolved().ProfilesRoot, inst.Name)
}

func (b windowsRedirect) roamingDir(inst config.Instance) string {
	return filepath.Join(b.HomeDir(inst), "AppData", "Roaming")
}

func (b windowsRedirect) localDir(inst config.Instance) string {
	return filepath.Join(b.HomeDir(inst), "AppData", "Local")
}

func (b windowsRedirect) documentsDir(inst config.Instance) string {
	return filepath.Join(b.HomeDir(inst), "Documents")
}

// DisplayUser 明确说明所有实例仍由当前 Windows 用户运行。
func (b windowsRedirect) DisplayUser(inst config.Instance) string {
	return "current-user"
}

// SharedDir 保持项目已有的共享目录约定；它不是微信 Profile 的一部分。
func (b windowsRedirect) SharedDir(inst config.Instance) string {
	return b.resolved().SharedDir
}

// environment 从当前进程完整复制环境，仅覆盖微信数据定位所需的变量。
// USERPROFILE、HOMEDRIVE、HOMEPATH 故意保持原值，避免把系统桌面会话伪装成
// 不完整的 Windows Profile；Documents 是否需重定向应由实际 PoC 决定。
func (b windowsRedirect) environment(inst config.Instance) []string {
	env := append([]string(nil), os.Environ()...)
	env = setWindowsEnv(env, "APPDATA", b.roamingDir(inst))
	env = setWindowsEnv(env, "LOCALAPPDATA", b.localDir(inst))
	env = setWindowsEnv(env, paths.InstanceEnvKey, inst.Name)
	return env
}

// setWindowsEnv 按 Windows 不区分大小写的变量名规则替换环境项。
func setWindowsEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		entryKey, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(entryKey, key) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// waitCurrentProcessAlive 在 detach 返回前检查启动器没有立即退出。
func waitCurrentProcessAlive(pid int) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return fmt.Errorf("wechat pid %d exited immediately; confirm the existing multi-open hook is active", pid)
		}
		time.Sleep(150 * time.Millisecond)
	}
	return nil
}

// terminateProcessTree uses taskkill's /T only after graceful closing timed out.
func terminateProcessTree(pid int) error {
	cmd := exec.Command("taskkill", "/PID", fmt.Sprint(pid), "/T", "/F")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
