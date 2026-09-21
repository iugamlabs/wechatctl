package wxdata

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

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

	probed map[Kind]bool
	dbs    []*sql.DB
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
		probed:   make(map[Kind]bool),
	}, nil
}

// OpenDB 解密并只读打开 relKey 对应库；每个 kind 只 Probe 一次。
func (s *Store) OpenDB(relKey string, kind Kind) (*sql.DB, error) {
	plain, err := s.Cache.Get(relKey)
	if err != nil {
		return nil, err
	}
	db, err := cache.OpenPlain(plain)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", relKey, ErrDecrypt)
	}
	if !s.probed[kind] {
		if err := Probe(db, kind); err != nil {
			db.Close()
			return nil, err
		}
		s.probed[kind] = true
	}
	s.dbs = append(s.dbs, db)
	return db, nil
}

// WithStateLock 在实例 lock 文件上排他锁（解密与 last_check 共用）。
func (s *Store) WithStateLock(fn func() error) error {
	lockPath := filepath.Join(s.StateDir, "lock")
	if err := os.MkdirAll(s.StateDir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

// LastCheckPath 返回 new-messages 游标文件路径。
func (s *Store) LastCheckPath() string {
	return filepath.Join(s.StateDir, "last_check.json")
}

// LoadLastCheck 读取 username -> last_timestamp 游标。
func (s *Store) LoadLastCheck() (map[string]int64, error) {
	return s.readLastCheck()
}

func (s *Store) readLastCheck() (map[string]int64, error) {
	path := s.LastCheckPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]int64{}, nil
		}
		return nil, err
	}
	var state map[string]int64
	if err := json.Unmarshal(data, &state); err != nil {
		return map[string]int64{}, nil
	}
	if state == nil {
		return map[string]int64{}, nil
	}
	return state, nil
}

func (s *Store) writeLastCheck(state map[string]int64) error {
	path := s.LastCheckPath()
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// SaveLastCheck 原子写入游标（0600）。
func (s *Store) SaveLastCheck(state map[string]int64) error {
	return s.WithStateLock(func() error {
		return s.writeLastCheck(state)
	})
}

// SaveLastCheckLocked 写入游标；调用方已通过 WithStateLock 持有实例锁。
func (s *Store) SaveLastCheckLocked(state map[string]int64) error {
	return s.writeLastCheck(state)
}

// RemoveLastCheck 删除游标（--reset）。
func (s *Store) RemoveLastCheck() error {
	err := os.Remove(s.LastCheckPath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Close 释放资源（不删除缓存文件）。
func (s *Store) Close() error {
	for _, db := range s.dbs {
		db.Close()
	}
	s.dbs = nil
	return nil
}
