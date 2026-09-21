//go:build linux

package paths

import (
	"os"
	"path/filepath"
)

// DesktopExt 是 Linux 桌面启动器后缀。
const DesktopExt = ".desktop"

// DefaultLayout 按 XDG 规范解析配置与数据目录。
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

func xdgConfigHome(home string) string {
	// root/sudo 时忽略 env_keep 的 XDG_*，避免落到 /root。
	if euidFn() != 0 {
		if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
			return v
		}
	}
	return filepath.Join(home, ".config")
}

func xdgDataHome(home string) string {
	if euidFn() != 0 {
		if v := os.Getenv("XDG_DATA_HOME"); v != "" {
			return v
		}
	}
	return filepath.Join(home, ".local", "share")
}
