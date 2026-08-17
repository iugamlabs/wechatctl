//go:build windows

package backend

import (
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	gwlStyle          = int32(-16)
	gwlExStyle        = int32(-20)
	wsCaption         = 0x00C00000
	wsThickFrame      = 0x00040000
	wsDlgFrame        = 0x00400000
	wsExClientEdge    = 0x00000200
	wsExStaticEdge    = 0x00020000
	wsExWindowEdge    = 0x00000100
	wsExDlgModalFrame = 0x00000001
	wsExToolWindow    = 0x00000080
	swpNoSize         = 0x0001
	swpNoMove         = 0x0002
	swpNoZOrder       = 0x0004
	swpNoActivate     = 0x0010
	swpFrameChanged   = 0x0020
	dwmncrpEnabled    = 2
	dwmwcpRound       = 2
	dwmwaColorNone    = 0xFFFFFFFE
	minPolishWidth    = 200
	minPolishHeight   = 200
)

var (
	procGetWindowLongPtrW = modUser32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW = modUser32.NewProc("SetWindowLongPtrW")
	procSetWindowPos      = modUser32.NewProc("SetWindowPos")
	procGetWindowRect     = modUser32.NewProc("GetWindowRect")
)

var polishEnumCB = syscall.NewCallback(enumPolishWindows)

var polishState struct {
	mu    sync.Mutex
	pids  map[uint32]struct{}
	count int
}

type winRect struct {
	Left, Top, Right, Bottom int32
}

// polishWeixinFrames 等待微信窗口出现后去掉经典灰框并启用 DWM 圆角。
// 隔离用户进程拿不到当前会话的视觉样式时，系统会给自定义标题栏再套一层灰色非客户区。
func polishWeixinFrames(rootPID uint32, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pids := processTree(rootPID)
		if len(pids) == 0 {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if n := enumAndPolish(pids); n > 0 {
			// 窗口可能稍后重建，再刷几次。
			time.Sleep(400 * time.Millisecond)
			enumAndPolish(processTree(rootPID))
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// processTree 返回 root 及其子孙进程 ID。
func processTree(root uint32) map[uint32]struct{} {
	out := map[uint32]struct{}{root: {}}
	if root == 0 {
		return out
	}
	for {
		added := false
		snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
		if err != nil {
			return out
		}
		var pe windows.ProcessEntry32
		pe.Size = uint32(unsafe.Sizeof(pe))
		for err := windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
			if _, ok := out[pe.ParentProcessID]; ok {
				if _, exists := out[pe.ProcessID]; !exists {
					out[pe.ProcessID] = struct{}{}
					added = true
				}
			}
		}
		windows.CloseHandle(snap)
		if !added {
			return out
		}
	}
}

// enumAndPolish 枚举顶层窗口并为匹配进程去掉灰框。
func enumAndPolish(pids map[uint32]struct{}) int {
	polishState.mu.Lock()
	polishState.pids = pids
	polishState.count = 0
	_ = windows.EnumWindows(polishEnumCB, nil)
	n := polishState.count
	polishState.mu.Unlock()
	return n
}

// enumPolishWindows 是 EnumWindows 回调。
func enumPolishWindows(hwnd windows.HWND, _ uintptr) uintptr {
	var pid uint32
	_, _ = windows.GetWindowThreadProcessId(hwnd, &pid)
	if _, ok := polishState.pids[pid]; !ok {
		return 1
	}
	if polishTopWindow(hwnd) {
		polishState.count++
	}
	return 1
}

// polishTopWindow 去掉隔离用户窗口上的经典非客户区灰框。
func polishTopWindow(hwnd windows.HWND) bool {
	if !windows.IsWindowVisible(hwnd) {
		return false
	}
	ex := getWindowLong(hwnd, gwlExStyle)
	if ex&wsExToolWindow != 0 {
		return false
	}
	var rc winRect
	if r1, _, _ := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc))); r1 == 0 {
		return false
	}
	if rc.Right-rc.Left < minPolishWidth || rc.Bottom-rc.Top < minPolishHeight {
		return false
	}

	style := getWindowLong(hwnd, gwlStyle)
	newStyle := style &^ uintptr(wsCaption|wsDlgFrame)
	newEx := ex &^ uintptr(wsExClientEdge|wsExStaticEdge|wsExWindowEdge|wsExDlgModalFrame)
	changed := false
	if newStyle != style {
		setWindowLong(hwnd, gwlStyle, newStyle)
		changed = true
	}
	if newEx != ex {
		setWindowLong(hwnd, gwlExStyle, newEx)
		changed = true
	}

	policy := uint32(dwmncrpEnabled)
	_ = windows.DwmSetWindowAttribute(hwnd, windows.DWMWA_NCRENDERING_POLICY, unsafe.Pointer(&policy), 4)
	none := uint32(dwmwaColorNone)
	_ = windows.DwmSetWindowAttribute(hwnd, windows.DWMWA_BORDER_COLOR, unsafe.Pointer(&none), 4)
	round := uint32(dwmwcpRound)
	_ = windows.DwmSetWindowAttribute(hwnd, windows.DWMWA_WINDOW_CORNER_PREFERENCE, unsafe.Pointer(&round), 4)

	if changed {
		procSetWindowPos.Call(uintptr(hwnd), 0, 0, 0, 0, 0, uintptr(swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged))
	} else {
		procSetWindowPos.Call(uintptr(hwnd), 0, 0, 0, 0, 0, uintptr(swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged))
	}
	return true
}

func getWindowLong(hwnd windows.HWND, index int32) uintptr {
	r, _, _ := procGetWindowLongPtrW.Call(uintptr(hwnd), uintptr(index))
	return r
}

func setWindowLong(hwnd windows.HWND, index int32, value uintptr) {
	procSetWindowLongPtrW.Call(uintptr(hwnd), uintptr(index), value)
}
