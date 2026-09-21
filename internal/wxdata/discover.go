package wxdata

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

var skipWXRoot = map[string]bool{
	"all_users": true,
	"WMPF":      true,
}

// DiscoverDBDir 在实例伪造 HOME 下定位 xwechat_files/*/db_storage。
func DiscoverDBDir(instanceHome string) (string, error) {
	root := filepath.Join(instanceHome, "xwechat_files")
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("no WeChat data under %s (instance never logged in?)", instanceHome)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return "", fmt.Errorf("no WeChat data under %s (instance never logged in?)", instanceHome)
	}

	var candidates []string
	for _, ent := range entries {
		if !ent.IsDir() || skipWXRoot[ent.Name()] {
			continue
		}
		dbStorage := filepath.Join(root, ent.Name(), "db_storage")
		if !hasDBStorageLayout(dbStorage) {
			continue
		}
		candidates = append(candidates, dbStorage)
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no WeChat data under %s (instance never logged in?)", instanceHome)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return dbStorageRank(candidates[i]) > dbStorageRank(candidates[j])
	})
	chosen := candidates[0]
	if len(candidates) > 1 {
		fmt.Fprintf(os.Stderr, "using db_storage %s (newest of %d)\n", chosen, len(candidates))
	}

	abs, err := filepath.Abs(chosen)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func hasDBStorageLayout(dbStorage string) bool {
	st, err := os.Stat(dbStorage)
	if err != nil || !st.IsDir() {
		return false
	}
	for _, sub := range []string{"session", "message", "contact"} {
		p := filepath.Join(dbStorage, sub)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

func dbStorageRank(dbStorage string) int64 {
	msg := filepath.Join(dbStorage, "message")
	if st, err := os.Stat(msg); err == nil {
		return st.ModTime().UnixNano()
	}
	if st, err := os.Stat(dbStorage); err == nil {
		return st.ModTime().UnixNano()
	}
	return 0
}
