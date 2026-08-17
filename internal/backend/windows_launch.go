//go:build windows

package backend

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
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

// terminatePID 强制结束单个进程，必要时回退到 taskkill。
func terminatePID(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err == nil {
		defer windows.CloseHandle(h)
		if err := windows.TerminateProcess(h, 1); err == nil {
			return nil
		}
	}
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	out, err2 := cmd.CombinedOutput()
	if err2 != nil {
		return fmt.Errorf("terminate pid %d: %v; taskkill: %w: %s", pid, err, err2, strings.TrimSpace(string(out)))
	}
	return nil
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

// userEnvironment 登录用户并构造环境块。
func userEnvironment(username, password, instance string) ([]uint16, error) {
	token, err := logonUser(username, ".", password)
	if err != nil {
		return nil, err
	}
	defer token.Close()
	return environmentFromToken(token, instance)
}

// environmentFromToken 在已加载 Profile 的令牌上生成环境块，并注入 WXCTL_INSTANCE。
func environmentFromToken(token windows.Token, instance string) ([]uint16, error) {
	var block *uint16
	if err := windows.CreateEnvironmentBlock(&block, token, false); err != nil {
		return nil, err
	}
	defer windows.DestroyEnvironmentBlock(block)
	return appendEnvUTF16(block, paths.InstanceEnvKey+"="+instance), nil
}

// hasUserLibDir 判断参数里是否已有 --user-lib-dir。
func hasUserLibDir(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "--user-lib-dir") {
			return true
		}
	}
	return false
}

// waitAliveOrFail 短暂等待后确认微信仍在运行；立即退出通常是单实例互斥或内核启动失败。
func waitAliveOrFail(proc windows.Handle, pid int) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var code uint32
		if err := windows.GetExitCodeProcess(proc, &code); err != nil {
			break
		}
		if code != stillActive {
			return fmt.Errorf("wechat pid %d exited immediately (status 0x%X). If another Weixin window is already open, its single-instance check may still apply across Windows users — close it and retry, or confirm your multi-open hook works for other users", pid, code)
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !processAlive(pid) {
		return fmt.Errorf("wechat pid %d is no longer running. If another Weixin window is already open, close it and retry", pid)
	}
	return nil
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
