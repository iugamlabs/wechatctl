//go:build windows

package backend

import "golang.org/x/sys/windows"

// isElevated 判断当前进程是否处于 UAC 提升后的管理员令牌。
func isElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
