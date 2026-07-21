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
	Home           string
	ConfigDir      string
	ConfigFile     string
	InstancesFile  string
	DataDir        string
	ProfilesRoot   string
	RunDir         string
	ApplicationsDir string
	SharedDir      string
	LegacyRoot     string
}

// DefaultLayout resolves XDG-based paths using the real user home.
func DefaultLayout() (Layout, error) {
	home, err := realHome()
	if err != nil {
		return Layout{}, err
	}

	configDir := filepath.Join(xdgConfigHome(home), AppName)
	dataDir := filepath.Join(xdgDataHome(home), AppName)
	appsDir := filepath.Join(xdgDataHome(home), "applications")

	return Layout{
		Home:            home,
		ConfigDir:       configDir,
		ConfigFile:      filepath.Join(configDir, "config.toml"),
		InstancesFile:   filepath.Join(configDir, "instances.toml"),
		DataDir:         dataDir,
		ProfilesRoot:    filepath.Join(dataDir, "instances"),
		RunDir:          filepath.Join(dataDir, "run"),
		ApplicationsDir: appsDir,
		SharedDir:       filepath.Join(home, "Documents", "WeChat-Shared"),
		LegacyRoot:      filepath.Join(xdgDataHome(home), LegacyProfilesDir),
	}, nil
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

func xdgConfigHome(home string) string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	return filepath.Join(home, ".config")
}

func xdgDataHome(home string) string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	return filepath.Join(home, ".local", "share")
}

// ExpandPath expands leading ~ to home.
func ExpandPath(home, p string) string {
	if p == "" {
		return p
	}
	if p == "~" {
		return home
	}
	if len(p) >= 2 && p[0] == '~' && p[1] == '/' {
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

// DesktopFile returns the .desktop path for an instance.
func (l Layout) DesktopFile(name string) string {
	return filepath.Join(l.ApplicationsDir, DesktopPrefix+name+".desktop")
}
