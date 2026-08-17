//go:build windows

package backend

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/star-plan/wechatctl/internal/paths"
	"golang.org/x/sys/windows"
)

var closeTargets struct {
	mu  sync.Mutex
	set map[uint32]struct{}
}

var enumCloseCB = syscall.NewCallback(enumCloseWindows)

type jobProcessIDList struct {
	NumberOfAssignedProcesses uint32
	NumberOfProcessIdsInList  uint32
	ProcessIdList             [128]uintptr
}

// openNamedJob 打开或创建 Local\wxctl_<name> 命名作业对象。
func openNamedJob(name string) (windows.Handle, error) {
	wname, err := windows.UTF16PtrFromString(`Local\wxctl_` + name)
	if err != nil {
		return 0, err
	}
	return windows.CreateJobObject(nil, wname)
}

// terminateJob 终止命名作业中的全部进程。
func terminateJob(name string) error {
	job, err := openNamedJob(name)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(job)
	return windows.TerminateJobObject(job, 1)
}

// jobPIDs 返回作业中仍关联的进程 ID。
func jobPIDs(name string) []uint32 {
	job, err := openNamedJob(name)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(job)
	var info jobProcessIDList
	err = windows.QueryInformationJobObject(
		job,
		windows.JobObjectBasicProcessIdList,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
		nil,
	)
	if err != nil && info.NumberOfProcessIdsInList == 0 {
		return nil
	}
	n := int(info.NumberOfProcessIdsInList)
	if n > len(info.ProcessIdList) {
		n = len(info.ProcessIdList)
	}
	out := make([]uint32, 0, n)
	for i := 0; i < n; i++ {
		if info.ProcessIdList[i] != 0 {
			out = append(out, uint32(info.ProcessIdList[i]))
		}
	}
	return out
}

// processAlive 判断 pid 是否仍在运行。
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

// terminatePID 强制结束单个进程。
func terminatePID(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

// requestClose 向目标进程的顶层窗口投递 WM_CLOSE。
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
		postMessage(hwnd, wmClose, 0, 0)
	}
	return 1
}

// waitJob 等到命名作业中已无进程，避免微信启动器提前退出导致 wxctl 误判结束。
func waitJob(name string, proc windows.Handle) error {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-interrupt:
			_ = terminateJob(name)
			_ = windows.TerminateProcess(proc, 1)
			return fmt.Errorf("interrupted")
		case <-ticker.C:
			if len(jobPIDs(name)) == 0 {
				return processExitError(proc)
			}
		}
	}
}

// processExitError 把进程退出码转成 ExitError。
func processExitError(proc windows.Handle) error {
	var code uint32
	if err := windows.GetExitCodeProcess(proc, &code); err != nil {
		return nil
	}
	if code == 0 || code == stillActive {
		return nil
	}
	return &ExitError{Code: int(code)}
}

// waitProcess 等待微信进程退出，并在 Ctrl+C 时终止作业。
func waitProcess(proc windows.Handle, name string) error {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	defer signal.Stop(interrupt)

	done := make(chan error, 1)
	go func() {
		if _, err := windows.WaitForSingleObject(proc, windows.INFINITE); err != nil {
			done <- err
			return
		}
		var code uint32
		if err := windows.GetExitCodeProcess(proc, &code); err != nil {
			done <- err
			return
		}
		if code == 0 {
			done <- nil
			return
		}
		done <- &ExitError{Code: int(code)}
	}()

	select {
	case <-interrupt:
		_ = terminateJob(name)
		_ = windows.TerminateProcess(proc, 1)
		<-done
		return fmt.Errorf("interrupted")
	case err := <-done:
		return err
	}
}

// userEnvironment 构造目标用户的环境块，并注入 WXCTL_INSTANCE。
func userEnvironment(username, password, instance string) ([]uint16, error) {
	token, err := logonUser(username, ".", password)
	if err != nil {
		return nil, err
	}
	defer token.Close()
	var block *uint16
	if err := windows.CreateEnvironmentBlock(&block, token, false); err != nil {
		return nil, err
	}
	defer windows.DestroyEnvironmentBlock(block)
	return appendEnvUTF16(block, paths.InstanceEnvKey+"="+instance), nil
}

// appendEnvUTF16 复制 UTF-16 环境块并追加额外条目。
func appendEnvUTF16(block *uint16, extra ...string) []uint16 {
	entries := splitEnvBlock(block)
	entries = append(entries, extra...)
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

// splitEnvBlock 将双 NUL 结尾的环境块拆成字符串列表。
func splitEnvBlock(block *uint16) []string {
	if block == nil {
		return nil
	}
	p := unsafe.Slice(block, 64*1024)
	var entries []string
	start := 0
	for i := 0; i < len(p); i++ {
		if p[i] != 0 {
			continue
		}
		if i == start {
			break
		}
		entries = append(entries, windows.UTF16ToString(p[start:i+1]))
		start = i + 1
	}
	return entries
}
