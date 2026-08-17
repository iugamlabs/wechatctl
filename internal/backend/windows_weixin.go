//go:build windows

package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// weixinRuntimeDirs 返回微信安装目录以及包含 Weixin.dll 的版本目录。
func weixinRuntimeDirs(wechatBin string) (installDir, versionDir string) {
	installDir = filepath.Dir(wechatBin)
	versionDir = findWeixinVersionDir(installDir)
	return installDir, versionDir
}

// findWeixinVersionDir 在安装目录下查找含 Weixin.dll 的最高版本子目录。
func findWeixinVersionDir(installDir string) string {
	entries, err := os.ReadDir(installDir)
	if err != nil {
		return ""
	}
	var bestName, bestPath string
	var bestParts []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		parts, ok := parseDottedVersion(name)
		if !ok {
			continue
		}
		p := filepath.Join(installDir, name)
		if _, err := os.Stat(filepath.Join(p, "Weixin.dll")); err != nil {
			continue
		}
		if bestName == "" || compareVersions(parts, bestParts) > 0 {
			bestName = name
			bestParts = parts
			bestPath = p
		}
	}
	return bestPath
}

// parseDottedVersion 解析 a.b.c.d 形式的版本号。
func parseDottedVersion(s string) ([]int, bool) {
	fields := strings.Split(s, ".")
	if len(fields) < 2 {
		return nil, false
	}
	out := make([]int, len(fields))
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

// compareVersions 比较两个点分版本，a>b 返回 1。
func compareVersions(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}

// prependPath 把目录插到环境块 PATH 最前面。
func prependPath(env []uint16, dirs ...string) []uint16 {
	var prefix []string
	for _, d := range dirs {
		if d != "" {
			prefix = append(prefix, d)
		}
	}
	if len(prefix) == 0 {
		return env
	}
	added := strings.Join(prefix, ";")
	if len(env) == 0 {
		return appendEnvUTF16(nil, "PATH="+added)
	}
	entries := splitEnvBlock(&env[0])
	found := false
	for i, e := range entries {
		if len(e) >= 5 && strings.EqualFold(e[:5], "PATH=") {
			entries[i] = "PATH=" + added + ";" + e[5:]
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, "PATH="+added)
	}
	return joinEnvEntries(entries)
}

// joinEnvEntries 把环境变量列表编码为双 NUL 结尾的 UTF-16 块。
func joinEnvEntries(entries []string) []uint16 {
	var buf []uint16
	for _, e := range entries {
		u, err := windows.UTF16FromString(e)
		if err != nil {
			continue
		}
		buf = append(buf, u...)
	}
	buf = append(buf, 0)
	return buf
}

// currentWeixinInstallMeta 读取当前用户 HKCU 中的微信安装信息。
func currentWeixinInstallMeta() (installPath string, version uint64) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Tencent\Weixin`, registry.QUERY_VALUE)
	if err != nil {
		return "", 0
	}
	defer k.Close()
	installPath, _, _ = k.GetStringValue("InstallPath")
	if v, _, err := k.GetIntegerValue("Version"); err == nil {
		version = v
	}
	return installPath, version
}

// seedWeixinHKCU 在已加载的用户 hive（HKU\<SID>）中写入微信安装信息。
func seedWeixinHKCU(username, installDir string) error {
	sid, _, _, err := windows.LookupSID("", username)
	if err != nil {
		return fmt.Errorf("lookup SID for %s: %w", username, err)
	}
	keyPath := sid.String() + `\Software\Tencent\Weixin`
	k, _, err := registry.CreateKey(registry.USERS, keyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open HKU Weixin key: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue("InstallPath", installDir); err != nil {
		return fmt.Errorf("set InstallPath: %w", err)
	}
	_, version := currentWeixinInstallMeta()
	if version == 0 {
		return nil
	}
	if err := k.SetDWordValue("Version", uint32(version)); err != nil {
		return fmt.Errorf("set Version: %w", err)
	}
	return nil
}
