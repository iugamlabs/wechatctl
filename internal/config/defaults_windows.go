//go:build windows

package config

import (
	"os"
	"path/filepath"
)

// defaultWechatBin 探测常见安装位置，找不到则返回新版微信默认路径。
func defaultWechatBin() string {
	for _, p := range wechatCandidates() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return `C:\Program Files\Tencent\Weixin\Weixin.exe`
}

// defaultIMModule Windows 不使用 Linux 输入法模块。
func defaultIMModule() string {
	return ""
}

// wechatCandidates 返回常见的微信安装路径。
func wechatCandidates() []string {
	roots := []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		`C:\Program Files`,
		`C:\Program Files (x86)`,
	}
	seen := make(map[string]struct{})
	var out []string
	for _, root := range roots {
		if root == "" {
			continue
		}
		for _, rel := range []string{
			filepath.Join("Tencent", "Weixin", "Weixin.exe"),
			filepath.Join("Tencent", "WeChat", "WeChat.exe"),
		} {
			p := filepath.Join(root, rel)
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}
