//go:build windows

package backend

import (
	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/paths"
)

// DefaultName 返回 Windows 当前用户数据目录重定向后端标识。
func DefaultName() string {
	return BackendWindowsRedirect
}

// newPlatformBackend 构造 Windows 的当前用户数据目录重定向后端。
func newPlatformBackend(layout paths.Layout, cfg config.Config) Backend {
	return windowsRedirect{layout: layout, cfg: cfg}
}
