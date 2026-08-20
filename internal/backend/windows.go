//go:build windows

package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/paths"
	"golang.org/x/sys/windows"
)

type windowsUser struct {
	layout paths.Layout
	cfg    config.Config
}

// DefaultName 返回 Windows 默认后端标识。
func DefaultName() string {
	return BackendWindowsUser
}

// newPlatformBackend 构造 Windows 本地用户隔离后端。
func newPlatformBackend(layout paths.Layout, cfg config.Config) Backend {
	return windowsUser{layout: layout, cfg: cfg}
}

func (b windowsUser) Name() string { return BackendWindowsUser }

func (b windowsUser) resolved() config.Config {
	return b.cfg.Resolve(b.layout)
}

func (b windowsUser) usernameOf(inst config.Instance) string {
	if inst.Username != "" {
		return inst.Username
	}
	return UsernameForInstance(inst.Name)
}

// Create 创建本地用户、初始化 Profile、配置共享目录 ACL，并用 DPAPI 保存密码。
func (b windowsUser) Create(inst config.Instance) (CreateResult, error) {
	if !isElevated() {
		return CreateResult{}, fmt.Errorf("creating a windows-user instance requires an elevated Administrator terminal")
	}
	username := b.usernameOf(inst)
	password, err := generatePassword(24)
	if err != nil {
		return CreateResult{}, err
	}

	if err := createOrResetLocalUser(username, password, "wxctl instance "+inst.Name); err != nil {
		return CreateResult{}, err
	}
	if sid, _, _, err := windows.LookupSID("", username); err == nil {
		_ = grantInteractiveDesktop(sid)
	}

	if err := withLoadedProfile(username, password, func(_ windows.Token, profileDir string) error {
		if bin := inst.EffectiveWechatBin(b.resolved()); bin != "" {
			installDir, _ := weixinRuntimeDirs(bin)
			if installDir != "" {
				_ = seedWeixinHKCU(username, installDir)
			}
		}
		if cur, err := currentUserSID(); err == nil {
			_ = grantModifySID(profileDir, cur)
		}
		_ = seedInteractiveHive(username)
		return nil
	}); err != nil {
		return CreateResult{}, fmt.Errorf("initialize profile for %s: %w", username, err)
	}
	if err := b.prepareShared(inst.Name, username); err != nil {
		return CreateResult{}, err
	}

	enc, err := protectPassword(inst.Name, password)
	if err != nil {
		return CreateResult{}, err
	}
	return CreateResult{
		Backend:           BackendWindowsUser,
		Username:          username,
		EncryptedPassword: enc,
	}, nil
}

// prepareShared 创建共享目录并授予主用户与实例用户读写权限。
func (b windowsUser) prepareShared(name, username string) error {
	root := b.resolved().SharedDir
	instShare := filepath.Join(root, name)
	for _, dir := range []string{root, instShare} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if cur, err := currentUserSID(); err == nil {
		_ = grantModifySID(root, cur)
		_ = grantModifySID(instShare, cur)
	}
	if err := grantModifyName(root, username); err != nil {
		return fmt.Errorf("grant shared dir ACL to %s: %w", username, err)
	}
	if err := grantModifyName(instShare, username); err != nil {
		return fmt.Errorf("grant instance share ACL to %s: %w", username, err)
	}
	return nil
}

// Start 以实例对应的 Windows 用户身份启动微信。
func (b windowsUser) Start(inst config.Instance, opts StartOptions) error {
	if inst.Backend != "" && inst.Backend != BackendWindowsUser {
		return fmt.Errorf("instance %q uses backend %s which is not supported on Windows; recreate the instance", inst.Name, inst.Backend)
	}
	username := b.usernameOf(inst)
	if inst.EncryptedPassword == "" {
		return fmt.Errorf("instance %q has no stored password; recreate it with wxctl create", inst.Name)
	}
	password, err := unprotectPassword(inst.Name, inst.EncryptedPassword)
	if err != nil {
		return fmt.Errorf("decrypt password for %s: %w", inst.Name, err)
	}

	cfg := b.resolved()
	bin := inst.EffectiveWechatBin(cfg)
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("wechat binary not found: %s (set with: wxctl config set wechat_bin <path>)", bin)
	}
	if err := os.MkdirAll(b.layout.RunDir, 0o755); err != nil {
		return err
	}

	// Weixin.exe 是启动器：会按工作目录 SetCurrentDirectory("4.x.x.x") 再 delay-load Weixin.dll。
	// 工作目录必须是安装目录，不能是用户 Profile。
	installDir, versionDir := weixinRuntimeDirs(bin)
	_ = addUserToUsersGroup(username)
	if sid, _, _, err := windows.LookupSID("", username); err == nil {
		if err := grantInteractiveDesktop(sid); err != nil {
			return fmt.Errorf("grant desktop access to %s: %w (the isolated user cannot show windows on this desktop)", username, err)
		}
	}
	env, err := userEnvironment(username, password, inst.Name)
	if err != nil {
		env = nil
	}
	_ = withLoadedProfile(username, password, func(token windows.Token, _ string) error {
		_ = seedWeixinHKCU(username, installDir)
		_ = seedInteractiveHive(username)
		if e, err := environmentFromToken(token, inst.Name); err == nil {
			env = e
		}
		return nil
	})
	env = prependPath(env, versionDir, installDir)
	_ = ensureCtfmon(username, password)

	cwd := installDir
	if _, err := os.Stat(cwd); err != nil {
		cwd = ""
	}

	// 微信 4 是 Chromium 内核：不能 CREATE_SUSPENDED，也不要放进 Job Object，否则会静默退出。
	args := append([]string{}, opts.ExtraArgs...)
	if versionDir != "" && !hasUserLibDir(args) {
		args = append(args, "--user-lib-dir="+versionDir)
	}
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT)
	cmdLine := windows.ComposeCommandLine(append([]string{bin}, args...))
	pi, err := createProcessWithLogon(username, ".", password, bin, cmdLine, env, cwd, flags, true)
	if err != nil {
		return fmt.Errorf("start wechat as %s: %w", username, err)
	}
	defer windows.CloseHandle(pi.Thread)

	pid := int(pi.ProcessId)
	if err := writePid(b.layout.PidFile(inst.Name), pid); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		windows.CloseHandle(pi.Process)
		return err
	}

	if err := SpawnFrameWatcher(uint32(pid)); err != nil {
		go polishWeixinFrames(uint32(pid), 8*time.Second)
	}

	if opts.Detach {
		if err := waitAliveOrFail(pi.Process, pid); err != nil {
			windows.CloseHandle(pi.Process)
			_ = os.Remove(b.layout.PidFile(inst.Name))
			return err
		}
		windows.CloseHandle(pi.Process)
		return nil
	}
	defer func() { _ = os.Remove(b.layout.PidFile(inst.Name)) }()
	defer windows.CloseHandle(pi.Process)
	return waitProcess(pi.Process, inst.Name)
}

// Stop 先尝试关闭窗口，超时后终止 Job Object。
func (b windowsUser) Stop(name string, timeout time.Duration) error {
	st := b.Status(name)
	if !st.Running {
		return fmt.Errorf("instance %q is not running", name)
	}
	pids := jobPIDs(name)
	if len(pids) == 0 && st.PID != 0 {
		pids = []uint32{uint32(st.PID)}
	}
	requestClose(pids)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !b.Status(name).Running {
			_ = os.Remove(b.layout.PidFile(name))
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := terminateJob(name); err != nil && st.PID != 0 {
		if err2 := terminatePID(st.PID); err2 != nil {
			return fmt.Errorf("stop instance %q: %v; terminate pid: %v", name, err, err2)
		}
	}
	time.Sleep(100 * time.Millisecond)
	_ = os.Remove(b.layout.PidFile(name))
	return nil
}

// Remove 在 purge 时删除本地用户、用户配置文件和实例共享目录。
func (b windowsUser) Remove(inst config.Instance, purge bool) error {
	if !purge {
		return nil
	}
	if !isElevated() {
		return fmt.Errorf("purging a windows-user instance requires an elevated Administrator terminal")
	}
	_ = b.Stop(inst.Name, 5*time.Second)
	username := b.usernameOf(inst)
	if err := deleteLocalUserAndProfile(username); err != nil {
		return err
	}
	_ = os.RemoveAll(filepath.Join(b.resolved().SharedDir, inst.Name))
	return nil
}

// Status 优先查询命名 Job Object 中的进程，回退到 pid 文件。
func (b windowsUser) Status(name string) Status {
	st := Status{Name: name}
	if pids := jobPIDs(name); len(pids) > 0 {
		st.Running = true
		st.PID = int(pids[0])
		return st
	}
	pid, err := readPid(b.layout.PidFile(name))
	if err != nil {
		if os.IsNotExist(err) {
			return st
		}
		st.Error = err.Error()
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

// HomeDir 返回实例 Windows 用户的 Profile 目录。
func (b windowsUser) HomeDir(inst config.Instance) string {
	return fallbackProfileDir(b.usernameOf(inst))
}

// DisplayUser 返回实例对应的本地用户名。
func (b windowsUser) DisplayUser(inst config.Instance) string {
	return b.usernameOf(inst)
}

// SharedDir 返回该实例的共享子目录。
func (b windowsUser) SharedDir(inst config.Instance) string {
	return filepath.Join(b.resolved().SharedDir, inst.Name)
}
