//go:build windows

package backend

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
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

	eventObjectCreate    = 0x8000
	eventObjectShow      = 0x8002
	wineventOutOfContext = 0x0000
	wineventSkipOwnProc  = 0x0002
	objidWindow          = 0
	wmQuit               = 0x0012
)

var (
	procGetWindowLongPtrW  = modUser32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW  = modUser32.NewProc("SetWindowLongPtrW")
	procSetWindowPos       = modUser32.NewProc("SetWindowPos")
	procGetWindowRect      = modUser32.NewProc("GetWindowRect")
	procSetWinEventHook    = modUser32.NewProc("SetWinEventHook")
	procUnhookWinEvent     = modUser32.NewProc("UnhookWinEvent")
	procGetMessageW        = modUser32.NewProc("GetMessageW")
	procDispatchMessageW   = modUser32.NewProc("DispatchMessageW")
	procPostThreadMessageW = modUser32.NewProc("PostThreadMessageW")
	procGetCurrentThreadId = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetCurrentThreadId")
	winEventCB             = syscall.NewCallback(winEventProc)
)

var polishEnumCB = syscall.NewCallback(enumPolishWindows)

var polishState struct {
	mu    sync.Mutex
	pids  map[uint32]struct{}
	count int
}

var watchState struct {
	mu   sync.Mutex
	root uint32
	tree map[uint32]struct{}
}

type winEventMSG struct {
	Hwnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

type winRect struct {
	Left, Top, Right, Bottom int32
}

// SpawnFrameWatcher 拉起隐藏的 wxctl _watch-frame，在微信退出前持续处理新建窗口。
// 登录窗和主界面是不同 HWND，启动后只轮询几秒无法覆盖登录之后的窗口。
func SpawnFrameWatcher(rootPID uint32) error {
	if rootPID == 0 {
		return fmt.Errorf("invalid pid")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "_watch-frame", strconv.FormatUint(uint64(rootPID), 10))
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// WatchWeixinFrames 在当前线程上挂接进程外 WinEvent，有窗口创建/显示就修边框，直到进程树退出。
func WatchWeixinFrames(rootPID uint32) error {
	if rootPID == 0 {
		return fmt.Errorf("invalid pid")
	}
	watchState.mu.Lock()
	watchState.root = rootPID
	watchState.tree = processTree(rootPID)
	watchState.mu.Unlock()

	hook, _, err := procSetWinEventHook.Call(
		uintptr(eventObjectCreate),
		uintptr(eventObjectShow),
		0,
		winEventCB,
		0,
		0,
		uintptr(wineventOutOfContext|wineventSkipOwnProc),
	)
	if hook == 0 {
		return fmt.Errorf("SetWinEventHook: %w", err)
	}
	defer procUnhookWinEvent.Call(hook)

	enumAndPolish(processTree(rootPID))

	tid, _, _ := procGetCurrentThreadId.Call()
	go func() {
		for {
			if !processTreeAlive(rootPID) {
				procPostThreadMessageW.Call(tid, uintptr(wmQuit), 0, 0)
				return
			}
			tree := processTree(rootPID)
			watchState.mu.Lock()
			watchState.tree = tree
			watchState.mu.Unlock()
			time.Sleep(time.Second)
		}
	}()

	var m winEventMSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	return nil
}

// winEventProc 处理窗口创建/显示；WINEVENT_OUTOFCONTEXT，不注入微信进程。
func winEventProc(_ uintptr, _ uint32, hwnd windows.HWND, idObject, _ int32, _, _ uint32) uintptr {
	if hwnd == 0 || idObject != objidWindow {
		return 0
	}
	var pid uint32
	_, _ = windows.GetWindowThreadProcessId(hwnd, &pid)
	if !watchOwnsPID(pid) {
		return 0
	}
	polishTopWindow(hwnd)
	return 0
}

// watchOwnsPID 判断窗口是否属于被监视的微信进程树。
func watchOwnsPID(pid uint32) bool {
	watchState.mu.Lock()
	_, ok := watchState.tree[pid]
	root := watchState.root
	watchState.mu.Unlock()
	if ok {
		return true
	}
	tree := processTree(root)
	watchState.mu.Lock()
	watchState.tree = tree
	_, ok = tree[pid]
	watchState.mu.Unlock()
	return ok
}

// processTreeAlive 判断根进程或其子孙是否仍在运行。
func processTreeAlive(root uint32) bool {
	if processAlive(int(root)) {
		return true
	}
	for pid := range processTree(root) {
		if pid != root && processAlive(int(pid)) {
			return true
		}
	}
	return false
}

// polishWeixinFrames 短时轮询修边框，仅作 watcher 拉起失败时的回退。
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
