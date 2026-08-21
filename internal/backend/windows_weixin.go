//go:build windows

package backend

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	var bestPath string
	var bestParts []int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		parts, ok := parseDottedVersion(e.Name())
		if !ok {
			continue
		}
		path := filepath.Join(installDir, e.Name())
		if _, err := os.Stat(filepath.Join(path, "Weixin.dll")); err != nil {
			continue
		}
		if bestPath == "" || compareVersions(parts, bestParts) > 0 {
			bestParts = parts
			bestPath = path
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
	for i, field := range fields {
		n, err := strconv.Atoi(field)
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
