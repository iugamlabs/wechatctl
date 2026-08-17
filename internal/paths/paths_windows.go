//go:build windows

package paths

import (
	"os"
	"path/filepath"
)

// DesktopExt 是 Windows 快捷方式后缀。
const DesktopExt = ".lnk"

// DefaultLayout 按 Windows 惯例解析配置、数据与共享目录。
func DefaultLayout() (Layout, error) {
	home, err := realHome()
	if err != nil {
		return Layout{}, err
	}

	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	publicDir := os.Getenv("PUBLIC")
	if publicDir == "" {
		publicDir = `C:\Users\Public`
	}

	configDir := filepath.Join(appData, AppName)
	dataDir := filepath.Join(localAppData, AppName)
	appsDir := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", AppName)

	return Layout{
		Home:            home,
		ConfigDir:       configDir,
		ConfigFile:      filepath.Join(configDir, "config.toml"),
		InstancesFile:   filepath.Join(configDir, "instances.toml"),
		DataDir:         dataDir,
		ProfilesRoot:    filepath.Join(dataDir, "instances"),
		RunDir:          filepath.Join(dataDir, "run"),
		ApplicationsDir: appsDir,
		SharedDir:       filepath.Join(publicDir, "wechatctl-share"),
		LegacyRoot:      filepath.Join(dataDir, LegacyProfilesDir),
	}, nil
}
