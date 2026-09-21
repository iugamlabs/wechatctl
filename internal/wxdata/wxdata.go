package wxdata

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/instance"
	"github.com/star-plan/wechatctl/internal/paths"
	"github.com/star-plan/wechatctl/internal/wxdata/cache"
	"github.com/star-plan/wechatctl/internal/wxdata/keys"
)

// Store 是单实例数据查询状态（密钥 + 解密缓存）。
type Store struct {
	Name     string
	Home     string
	DBDir    string
	StateDir string
	Keys     map[string]keys.KeyInfo
	Cache    *cache.Cache
}

// Open 加载实例密钥并准备解密缓存（不探测微信是否在跑）。
func Open(layout paths.Layout, cfg config.Config, name string) (*Store, error) {
	if err := config.ValidateName(name); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	mgr := instance.Manager{Layout: layout, Config: cfg.Resolve(layout)}
	if _, err := mgr.Get(name); err != nil {
		return nil, fmt.Errorf("instance %q not found: %w", name, ErrInstanceNotFound)
	}
	home := mgr.HomeDir(name)
	dbDir, err := DiscoverDBDir(home)
	if err != nil {
		return nil, err
	}

	stateDir := layout.WxdataDir(name)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}

	keysPath := keys.KeysPath(stateDir)
	if _, err := os.Stat(keysPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("run: wxctl init-data %s: %w", name, ErrNoKeys)
		}
		return nil, err
	}

	storedDir, keyMap, err := keys.Load(keysPath)
	if err != nil {
		return nil, err
	}
	storedDir, err = filepath.EvalSymlinks(storedDir)
	if err != nil {
		return nil, fmt.Errorf("db_storage changed (wxid rotated?); run: wxctl init-data --force %s: %w", name, ErrNoKeys)
	}
	if storedDir == "" || storedDir != dbDir {
		return nil, fmt.Errorf("db_storage changed (wxid rotated?); run: wxctl init-data --force %s: %w", name, ErrNoKeys)
	}

	c, err := cache.New(keyMap, dbDir, filepath.Join(stateDir, "cache"))
	if err != nil {
		return nil, err
	}

	return &Store{
		Name:     name,
		Home:     home,
		DBDir:    dbDir,
		StateDir: stateDir,
		Keys:     keyMap,
		Cache:    c,
	}, nil
}

// Close 释放资源（不删除缓存文件）。
func (s *Store) Close() error {
	return nil
}
