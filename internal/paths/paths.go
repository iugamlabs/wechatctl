package paths

import (
	"os"
	"path/filepath"
)

const (
	AppName           = "wxctl"
	LegacyProfilesDir = "wechat-profiles"
	DesktopPrefix     = "wxctl-"
	InstanceEnvKey    = "WXCTL_INSTANCE"
)

// Layout holds resolved filesystem paths for wxctl.
type Layout struct {
	Home            string
	ConfigDir       string
	ConfigFile      string
	InstancesFile   string
	DataDir         string
	ProfilesRoot    string
	RunDir          string
	ApplicationsDir string
	SharedDir       string
	LegacyRoot      string
}

func realHome() (string, error) {
	if h := os.Getenv("WXCTL_REAL_HOME"); h != "" {
		return h, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return h, nil
}

// ExpandPath 将前导 ~ 展开为 home，同时接受 / 与 \。
func ExpandPath(home, p string) string {
	if p == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if len(p) >= 2 && p[0] == '~' && (p[1] == '/' || p[1] == '\\') {
		return filepath.Join(home, p[2:])
	}
	return p
}

// InstanceHome returns the fake HOME for an instance.
func (l Layout) InstanceHome(name string) string {
	return filepath.Join(l.ProfilesRoot, name)
}

// PidFile returns the pidfile path for an instance.
func (l Layout) PidFile(name string) string {
	return filepath.Join(l.RunDir, name+".pid")
}

// DesktopFile 返回实例 .desktop 启动器路径。
func (l Layout) DesktopFile(name string) string {
	return filepath.Join(l.ApplicationsDir, DesktopPrefix+name+DesktopExt)
}
