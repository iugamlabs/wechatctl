package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/deali/wxctl/internal/config"
	"github.com/deali/wxctl/internal/paths"
)

// Status describes whether an instance process is running.
type Status struct {
	Name    string
	Running bool
	PID     int
	Error   string
}

// Manager handles process start/stop/status.
type Manager struct {
	Layout paths.Layout
	Config config.Config
}

func (m Manager) resolved() config.Config {
	return m.Config.Resolve(m.Layout)
}

func (m Manager) instanceHome(name string) string {
	return filepath.Join(m.resolved().ProfilesRoot, name)
}

// Start runs WeChat in the foreground for the given instance and waits.
func (m Manager) Start(inst config.Instance, extraArgs []string) error {
	cfg := m.resolved()
	if err := os.MkdirAll(m.Layout.RunDir, 0o755); err != nil {
		return err
	}
	home := m.instanceHome(inst.Name)
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	shared := cfg.SharedDir
	if err := os.MkdirAll(shared, 0o755); err != nil {
		return err
	}
	link := filepath.Join(home, "Shared")
	if _, err := os.Lstat(link); os.IsNotExist(err) {
		if err := os.Symlink(shared, link); err != nil {
			return err
		}
	}

	bin := inst.EffectiveWechatBin(cfg)
	im := inst.EffectiveIMModule(cfg)

	cmd := exec.Command(bin, extraArgs...)
	cmd.Dir = home
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = buildEnv(home, inst.Name, im)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start wechat: %w", err)
	}
	pid := cmd.Process.Pid
	if err := writePid(m.Layout.PidFile(inst.Name), pid); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	defer func() { _ = os.Remove(m.Layout.PidFile(inst.Name)) }()

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

// ExitError preserves the child process exit code for the CLI.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("wechat exited with status %d", e.Code)
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

func writePid(path string, pid int) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readPid(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// Probe returns running status for one instance.
func (m Manager) Probe(name string) Status {
	st := Status{Name: name}
	pidPath := m.Layout.PidFile(name)
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

// StatusAll probes many instances.
func (m Manager) StatusAll(names []string) []Status {
	out := make([]Status, 0, len(names))
	for _, n := range names {
		out = append(out, m.Probe(n))
	}
	return out
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
		// If we cannot read environ, fall back to trusting pidfile while process is alive.
		return true
	}
	needle := []byte(paths.InstanceEnvKey + "=" + name)
	parts := strings.Split(string(data), "\x00")
	for _, p := range parts {
		if p == string(needle) {
			return true
		}
	}
	// Also accept if HOME points at this instance path — soft check via cmdline HOME not available;
	// without env match, reject to avoid killing unrelated processes.
	return false
}

// Stop sends SIGTERM then SIGKILL to the instance process.
func (m Manager) Stop(name string, timeout time.Duration) error {
	st := m.Probe(name)
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
			_ = os.Remove(m.Layout.PidFile(name))
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := proc.Signal(syscall.SIGKILL); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	_ = os.Remove(m.Layout.PidFile(name))
	return nil
}
