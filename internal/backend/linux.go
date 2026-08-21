//go:build !windows

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
)

type linuxHome struct {
	layout paths.Layout
	cfg    config.Config
}

// DefaultName 返回 Linux 默认后端标识。
func DefaultName() string {
	return BackendLinuxHome
}

// newPlatformBackend 构造 Linux HOME 隔离后端。
func newPlatformBackend(layout paths.Layout, cfg config.Config) Backend {
	return linuxHome{layout: layout, cfg: cfg}
}

func (b linuxHome) Name() string { return BackendLinuxHome }

func (b linuxHome) resolved() config.Config {
	return b.cfg.Resolve(b.layout)
}

// Create 创建实例伪造 HOME，并链接到共享目录。
func (b linuxHome) Create(inst config.Instance) (CreateResult, error) {
	home := b.HomeDir(inst)
	if err := os.MkdirAll(home, 0o755); err != nil {
		return CreateResult{}, err
	}
	if err := ensureSharedLink(home, b.resolved().SharedDir); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{Backend: BackendLinuxHome}, nil
}

// Start 以前台或分离方式启动微信。
func (b linuxHome) Start(inst config.Instance, opts StartOptions) error {
	if inst.Backend != "" && inst.Backend != BackendLinuxHome {
		return fmt.Errorf("instance %q uses backend %s which is not supported on this platform", inst.Name, inst.Backend)
	}
	cfg := b.resolved()
	if err := os.MkdirAll(b.layout.RunDir, 0o755); err != nil {
		return err
	}
	home := b.HomeDir(inst)
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	if err := ensureSharedLink(home, cfg.SharedDir); err != nil {
		return err
	}

	bin := inst.EffectiveWechatBin(cfg)
	im := inst.EffectiveIMModule(cfg)
	cmd := exec.Command(bin, opts.ExtraArgs...)
	cmd.Dir = home
	cmd.Env = buildEnv(home, inst.Name, im)
	if opts.Detach {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		cmd.Stdin = nil
		cmd.Stdout = nil
		cmd.Stderr = nil
	} else {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start wechat: %w", err)
	}
	if err := writePid(b.layout.PidFile(inst.Name), cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	if opts.Detach {
		return nil
	}
	defer func() { _ = os.Remove(b.layout.PidFile(inst.Name)) }()

	err := cmd.Wait()
	if err == nil {
		return nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return &ExitError{Code: status.ExitStatus()}
		}
	}
	return err
}

// Stop 向实例进程发送 SIGTERM，超时后 SIGKILL。
func (b linuxHome) Stop(name string, timeout time.Duration) error {
	st := b.Status(name)
	if !st.Running {
		return fmt.Errorf("instance %q is not running", name)
	}
	proc, err := os.FindProcess(st.PID)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !alive(st.PID) {
			_ = os.Remove(b.layout.PidFile(name))
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	_ = os.Remove(b.layout.PidFile(name))
	return nil
}

// Remove 在 purge 时删除实例 HOME。
func (b linuxHome) Remove(inst config.Instance, purge bool) error {
	if !purge {
		return nil
	}
	home := b.HomeDir(inst)
	if _, err := os.Stat(home); os.IsNotExist(err) {
		return nil
	}
	return os.RemoveAll(home)
}

// Status 通过 pid 文件和 /proc 环境变量确认进程归属。
func (b linuxHome) Status(name string) Status {
	st := Status{Name: name}
	pidPath := b.layout.PidFile(name)
	pid, err := readPid(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return st
		}
		st.Error = err.Error()
		return st
	}
	if !alive(pid) || !belongsToInstance(pid, name) {
		_ = os.Remove(pidPath)
		return st
	}
	st.Running = true
	st.PID = pid
	return st
}

// HomeDir 返回实例伪造 HOME。
func (b linuxHome) HomeDir(inst config.Instance) string {
	return filepath.Join(b.resolved().ProfilesRoot, inst.Name)
}

// DisplayUser Linux 下没有独立系统用户。
func (b linuxHome) DisplayUser(inst config.Instance) string {
	return "-"
}

// SharedDir 返回全局共享目录。
func (b linuxHome) SharedDir(inst config.Instance) string {
	return b.resolved().SharedDir
}

func ensureSharedLink(instanceHome, sharedDir string) error {
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		return err
	}
	link := filepath.Join(instanceHome, "Shared")
	if _, err := os.Lstat(link); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Symlink(sharedDir, link)
}

func buildEnv(home, name, imModule string) []string {
	env := os.Environ()
	env = setEnv(env, "HOME", home)
	env = setEnv(env, paths.InstanceEnvKey, name)
	if imModule != "" {
		env = setEnv(env, "GTK_IM_MODULE", imModule)
		env = setEnv(env, "QT_IM_MODULE", imModule)
		env = setEnv(env, "XMODIFIERS", "@im="+imModule)
	}
	return env
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func alive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func belongsToInstance(pid int, name string) bool {
	environPath := fmt.Sprintf("/proc/%d/environ", pid)
	data, err := os.ReadFile(environPath)
	if err != nil {
		return true
	}
	needle := paths.InstanceEnvKey + "=" + name
	for _, p := range strings.Split(string(data), "\x00") {
		if p == needle {
			return true
		}
	}
	return false
}
