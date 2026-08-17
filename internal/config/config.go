package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/star-plan/wechatctl/internal/paths"
)

// Config is the global wxctl configuration.
type Config struct {
	WechatBin    string `toml:"wechat_bin"`
	ProfilesRoot string `toml:"profiles_root"`
	SharedDir    string `toml:"shared_dir"`
	IMModule     string `toml:"im_module"`
}

// Default 返回当前平台的内置默认配置。
func Default(layout paths.Layout) Config {
	return Config{
		WechatBin:    defaultWechatBin(),
		ProfilesRoot: layout.ProfilesRoot,
		SharedDir:    layout.SharedDir,
		IMModule:     defaultIMModule(),
	}
}

// Load reads config.toml, creating defaults if missing.
func Load(layout paths.Layout) (Config, error) {
	cfg := Default(layout)
	data, err := os.ReadFile(layout.ConfigFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return Config{}, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults(layout)
	return cfg, nil
}

func (c *Config) applyDefaults(layout paths.Layout) {
	d := Default(layout)
	if c.WechatBin == "" {
		c.WechatBin = d.WechatBin
	}
	if c.ProfilesRoot == "" {
		c.ProfilesRoot = d.ProfilesRoot
	}
	if c.SharedDir == "" {
		c.SharedDir = d.SharedDir
	}
	if c.IMModule == "" {
		c.IMModule = d.IMModule
	}
}

// Resolve expands paths relative to the real home and returns a layout-aware copy.
func (c Config) Resolve(layout paths.Layout) Config {
	out := c
	out.ProfilesRoot = paths.ExpandPath(layout.Home, c.ProfilesRoot)
	out.SharedDir = paths.ExpandPath(layout.Home, c.SharedDir)
	out.WechatBin = paths.ExpandPath(layout.Home, c.WechatBin)
	return out
}

// Save writes config.toml.
func Save(layout paths.Layout, cfg Config) error {
	if err := os.MkdirAll(layout.ConfigDir, 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp := layout.ConfigFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, layout.ConfigFile)
}

// EnsureDirs creates profiles root, shared dir, run dir, and applications dir.
func EnsureDirs(layout paths.Layout, cfg Config) error {
	resolved := cfg.Resolve(layout)
	for _, dir := range []string{
		layout.ConfigDir,
		resolved.ProfilesRoot,
		resolved.SharedDir,
		layout.RunDir,
		layout.ApplicationsDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// GetField returns a string config field by key.
func GetField(cfg Config, key string) (string, error) {
	switch key {
	case "wechat_bin":
		return cfg.WechatBin, nil
	case "profiles_root":
		return cfg.ProfilesRoot, nil
	case "shared_dir":
		return cfg.SharedDir, nil
	case "im_module":
		return cfg.IMModule, nil
	default:
		return "", fmt.Errorf("unknown config key %q (want wechat_bin, profiles_root, shared_dir, im_module)", key)
	}
}

// SetField sets a string config field by key.
func SetField(cfg *Config, key, value string) error {
	switch key {
	case "wechat_bin":
		cfg.WechatBin = value
	case "profiles_root":
		cfg.ProfilesRoot = value
	case "shared_dir":
		cfg.SharedDir = value
	case "im_module":
		cfg.IMModule = value
	default:
		return fmt.Errorf("unknown config key %q (want wechat_bin, profiles_root, shared_dir, im_module)", key)
	}
	return nil
}

// RelativizeForSave stores paths with ~ when under home for readability.
func RelativizeForSave(home string, cfg Config) Config {
	out := cfg
	out.ProfilesRoot = maybeTilde(home, cfg.ProfilesRoot)
	out.SharedDir = maybeTilde(home, cfg.SharedDir)
	return out
}

func maybeTilde(home, p string) string {
	if p == home {
		return "~"
	}
	prefix := home + string(filepath.Separator)
	if len(p) > len(prefix) && p[:len(prefix)] == prefix {
		return "~/" + p[len(prefix):]
	}
	return p
}
