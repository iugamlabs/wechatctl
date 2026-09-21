package keys

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	metaDBDir = "_db_dir"
	keysFile  = "all_keys.json"
)

// KeyInfo 对应 all_keys.json 中单个库条目。
type KeyInfo struct {
	EncKey  string  `json:"enc_key,omitempty"`
	Salt    string  `json:"salt,omitempty"`
	SizeMB  float64 `json:"size_mb,omitempty"`
}

// HexMemoryRE 对齐 wechat-cli scanner_linux 内存 hex 模式。
var HexMemoryRE = regexp.MustCompile(`x'([0-9a-fA-F]{64,192})'`)

// StripMetadata 去掉 `_` 前缀元数据键。
func StripMetadata(m map[string]KeyInfo) map[string]KeyInfo {
	out := make(map[string]KeyInfo, len(m))
	for k, v := range m {
		if strings.HasPrefix(k, "_") {
			continue
		}
		out[k] = v
	}
	return out
}

// KeysPath 返回实例 all_keys.json 路径。
func KeysPath(stateDir string) string {
	return filepath.Join(stateDir, keysFile)
}

// Load 读取 all_keys.json，返回 _db_dir 与库条目（键已规范为正斜杠）。
func Load(path string) (dbDir string, entries map[string]KeyInfo, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", nil, fmt.Errorf("parse keys: %w", err)
	}
	entries = make(map[string]KeyInfo)
	for k, v := range raw {
		if k == metaDBDir {
			if err := json.Unmarshal(v, &dbDir); err != nil {
				return "", nil, fmt.Errorf("parse _db_dir: %w", err)
			}
			continue
		}
		if strings.HasPrefix(k, "_") {
			continue
		}
		var info KeyInfo
		if err := json.Unmarshal(v, &info); err != nil {
			return "", nil, fmt.Errorf("parse key %q: %w", k, err)
		}
		entries[normalizeRelKey(k)] = info
	}
	return dbDir, entries, nil
}

// Save 原子写入 all_keys.json（0600），含 _db_dir。
func Save(path, dbDir string, entries map[string]KeyInfo) error {
	out := make(map[string]interface{}, len(entries)+1)
	out[metaDBDir] = dbDir
	for k, v := range entries {
		out[normalizeRelKey(k)] = v
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalizeRelKey(rel string) string {
	return filepath.ToSlash(rel)
}

func isSafeRelPath(rel string) bool {
	normalized := strings.ReplaceAll(rel, "\\", "/")
	clean := path.Clean(normalized)
	for _, part := range strings.Split(clean, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// PathVariants 对齐 key_utils.key_path_variants。
func PathVariants(relPath string) []string {
	normalized := strings.ReplaceAll(relPath, "\\", "/")
	variants := []string{relPath, normalized}
	add := func(s string) {
		for _, v := range variants {
			if v == s {
				return
			}
		}
		variants = append(variants, s)
	}
	add(strings.ReplaceAll(normalized, "/", "\\"))
	add(filepath.FromSlash(normalized))
	return variants
}

// GetKeyInfo 按路径变体查找库密钥；拒绝 `..`。
func GetKeyInfo(m map[string]KeyInfo, relPath string) (KeyInfo, bool) {
	if !isSafeRelPath(relPath) {
		return KeyInfo{}, false
	}
	for _, candidate := range PathVariants(relPath) {
		if strings.HasPrefix(candidate, "_") {
			continue
		}
		if info, ok := m[normalizeRelKey(candidate)]; ok {
			return info, true
		}
		if info, ok := m[candidate]; ok {
			return info, true
		}
	}
	return KeyInfo{}, false
}

// ClassifyHex 解析内存 hex 命中（64 / 96 / 更长偶数）。
func ClassifyHex(hex string) (encKey, salt string, ok bool) {
	n := len(hex)
	if n < 64 || n%2 != 0 {
		return "", "", false
	}
	switch {
	case n == 96:
		return hex[:64], hex[64:], true
	case n == 64:
		return hex, "", true
	case n > 96:
		return hex[:64], hex[n-32:], true
	default:
		return "", "", false
	}
}
