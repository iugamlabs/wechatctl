//go:build windows

package backend

import (
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

const (
	wmClose     = 0x0010
	stillActive = 259
)

var closeTargets struct {
	mu  sync.Mutex
	set map[uint32]struct{}
}

var enumCloseCB = syscall.NewCallback(enumCloseWindows)

var procPostMessageW = windows.NewLazySystemDLL("user32.dll").NewProc("PostMessageW")

// processAlive 判断当前用户启动器 PID 是否仍在运行。
func processAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// requestClose 向实例进程树的顶层窗口投递 WM_CLOSE，优先让微信自行退出。
func requestClose(pids []uint32) {
	if len(pids) == 0 {
		return
	}
	set := make(map[uint32]struct{}, len(pids))
	for _, p := range pids {
		set[p] = struct{}{}
	}
	closeTargets.mu.Lock()
	closeTargets.set = set
	defer func() {
		closeTargets.set = nil
		closeTargets.mu.Unlock()
	}()
	_ = windows.EnumWindows(enumCloseCB, nil)
}

// enumCloseWindows 是 EnumWindows 回调，按 PID 匹配后发送 WM_CLOSE。
func enumCloseWindows(hwnd windows.HWND, _ uintptr) uintptr {
	var pid uint32
	_, _ = windows.GetWindowThreadProcessId(hwnd, &pid)
	if _, ok := closeTargets.set[pid]; ok {
		// PostMessageW 异步投递关闭请求，不会卡住 wxctl 的 stop 命令。
		procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
	}
	return 1
}

// hasUserLibDir 判断调用方是否已经显式指定版本目录。
func hasUserLibDir(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "--user-lib-dir") {
			return true
		}
	}
	return false
}
