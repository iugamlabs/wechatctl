//go:build windows

package backend

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	logon32LogonInteractive = 2
	logon32ProviderDefault  = 0
	logonWithProfile        = 0x00000001
	wmClose                 = 0x0010
	stillActive             = 259

	nerrUserNotFound     syscall.Errno = 2221
	nerrUserExists       syscall.Errno = 2224
	nerrBadUsername      syscall.Errno = 2202
	nerrPasswordTooShort syscall.Errno = 2245
	errorMemberInAlias   syscall.Errno = 1378

	userPrivUser       = 1
	ufScript           = 0x0001
	ufNormalAccount    = 0x0200
	ufDontExpirePasswd = 0x10000
	ufPasswdCantChange = 0x0040

	createNoWindow   = 0x08000000
	detachedProcess  = 0x00000008
	piNoUI           = 0x00000001
	winstaAllAccess  = 0x000F037F
	desktopAllAccess = 0x000F01FF
)

var (
	modAdvapi32 = windows.NewLazySystemDLL("advapi32.dll")
	modNetapi32 = windows.NewLazySystemDLL("netapi32.dll")
	modUserenv  = windows.NewLazySystemDLL("userenv.dll")
	modUser32   = windows.NewLazySystemDLL("user32.dll")

	procLogonUserW              = modAdvapi32.NewProc("LogonUserW")
	procCreateProcessWithLogonW = modAdvapi32.NewProc("CreateProcessWithLogonW")
	procNetUserAdd              = modNetapi32.NewProc("NetUserAdd")
	procNetUserDel              = modNetapi32.NewProc("NetUserDel")
	procNetUserSetInfo          = modNetapi32.NewProc("NetUserSetInfo")
	procNetLocalGroupAddMembers = modNetapi32.NewProc("NetLocalGroupAddMembers")
	procDeleteProfileW          = modUserenv.NewProc("DeleteProfileW")
	procLoadUserProfileW        = modUserenv.NewProc("LoadUserProfileW")
	procUnloadUserProfile       = modUserenv.NewProc("UnloadUserProfile")
	procPostMessageW            = modUser32.NewProc("PostMessageW")
	procOpenWindowStationW      = modUser32.NewProc("OpenWindowStationW")
	procCloseWindowStation      = modUser32.NewProc("CloseWindowStation")
	procGetProcessWindowStation = modUser32.NewProc("GetProcessWindowStation")
	procOpenDesktopW            = modUser32.NewProc("OpenDesktopW")
	procCloseDesktop            = modUser32.NewProc("CloseDesktop")
)

type userInfo1 struct {
	Name        *uint16
	Password    *uint16
	PasswordAge uint32
	Priv        uint32
	HomeDir     *uint16
	Comment     *uint16
	Flags       uint32
	ScriptPath  *uint16
}

type userInfo1003 struct {
	Password *uint16
}

type localGroupMembersInfo3 struct {
	DomainAndName *uint16
}

type profileInfoW struct {
	Size        uint32
	Flags       uint32
	UserName    *uint16
	ProfilePath *uint16
	DefaultPath *uint16
	ServerName  *uint16
	PolicyPath  *uint16
	Profile     windows.Handle
}

// logonUser 以交互方式登录本地用户并返回令牌。
func logonUser(username, domain, password string) (windows.Token, error) {
	var token windows.Token
	userp, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return 0, err
	}
	domp, err := windows.UTF16PtrFromString(domain)
	if err != nil {
		return 0, err
	}
	passp, err := windows.UTF16PtrFromString(password)
	if err != nil {
		return 0, err
	}
	r1, _, e := procLogonUserW.Call(
		uintptr(unsafe.Pointer(userp)),
		uintptr(unsafe.Pointer(domp)),
		uintptr(unsafe.Pointer(passp)),
		uintptr(logon32LogonInteractive),
		uintptr(logon32ProviderDefault),
		uintptr(unsafe.Pointer(&token)),
	)
	if r1 == 0 {
		return 0, fmt.Errorf("LogonUser: %w", e)
	}
	return token, nil
}

// createProcessWithLogon 以指定用户加载 Profile 并创建进程。
// attachDesktop 为 true 时绑定当前交互桌面（GUI 程序需要）；后台辅助进程应传 false。
func createProcessWithLogon(username, domain, password, appName, cmdLine string, env []uint16, cwd string, flags uint32, attachDesktop bool) (windows.ProcessInformation, error) {
	var pi windows.ProcessInformation
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	if attachDesktop {
		desktop, err := windows.UTF16PtrFromString(`winsta0\default`)
		if err != nil {
			return pi, err
		}
		si.Desktop = desktop
		si.Flags = windows.STARTF_USESHOWWINDOW
		si.ShowWindow = windows.SW_SHOWNORMAL
	} else {
		flags |= createNoWindow | detachedProcess
	}

	userp, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return pi, err
	}
	domp, err := windows.UTF16PtrFromString(domain)
	if err != nil {
		return pi, err
	}
	passp, err := windows.UTF16PtrFromString(password)
	if err != nil {
		return pi, err
	}
	var appp *uint16
	if appName != "" {
		appp, err = windows.UTF16PtrFromString(appName)
		if err != nil {
			return pi, err
		}
	}
	cmdp, err := windows.UTF16PtrFromString(cmdLine)
	if err != nil {
		return pi, err
	}
	var cwdp *uint16
	if cwd != "" {
		cwdp, err = windows.UTF16PtrFromString(cwd)
		if err != nil {
			return pi, err
		}
	}
	var envp *uint16
	if len(env) > 0 {
		envp = &env[0]
	}

	r1, _, e := procCreateProcessWithLogonW.Call(
		uintptr(unsafe.Pointer(userp)),
		uintptr(unsafe.Pointer(domp)),
		uintptr(unsafe.Pointer(passp)),
		uintptr(logonWithProfile),
		uintptr(unsafe.Pointer(appp)),
		uintptr(unsafe.Pointer(cmdp)),
		uintptr(flags),
		uintptr(unsafe.Pointer(envp)),
		uintptr(unsafe.Pointer(cwdp)),
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if r1 == 0 {
		return pi, fmt.Errorf("CreateProcessWithLogonW: %w", e)
	}
	runtime.KeepAlive(env)
	return pi, nil
}

// netUserAdd 创建本地普通用户，密码永不过期且不可自行修改。
func netUserAdd(username, password, comment string) error {
	namep, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return err
	}
	passp, err := windows.UTF16PtrFromString(password)
	if err != nil {
		return err
	}
	commentp, err := windows.UTF16PtrFromString(comment)
	if err != nil {
		return err
	}
	ui := userInfo1{
		Name:     namep,
		Password: passp,
		Priv:     userPrivUser,
		Comment:  commentp,
		Flags:    ufScript | ufNormalAccount | ufDontExpirePasswd | ufPasswdCantChange,
	}
	var parmErr uint32
	r0, _, _ := procNetUserAdd.Call(0, 1, uintptr(unsafe.Pointer(&ui)), uintptr(unsafe.Pointer(&parmErr)))
	if r0 != 0 {
		return mapNetUserError(syscall.Errno(r0))
	}
	return nil
}

// netUserDel 删除本地用户。
func netUserDel(username string) error {
	namep, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return err
	}
	r0, _, _ := procNetUserDel.Call(0, uintptr(unsafe.Pointer(namep)))
	if r0 != 0 {
		return mapNetUserError(syscall.Errno(r0))
	}
	return nil
}

// netUserSetPassword 重置已存在用户的密码。
func netUserSetPassword(username, password string) error {
	namep, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return err
	}
	passp, err := windows.UTF16PtrFromString(password)
	if err != nil {
		return err
	}
	info := userInfo1003{Password: passp}
	var parmErr uint32
	r0, _, _ := procNetUserSetInfo.Call(
		0,
		uintptr(unsafe.Pointer(namep)),
		1003,
		uintptr(unsafe.Pointer(&info)),
		uintptr(unsafe.Pointer(&parmErr)),
	)
	if r0 != 0 {
		return mapNetUserError(syscall.Errno(r0))
	}
	return nil
}

// netLocalGroupAddMember 将用户加入本地组；已是成员时视为成功。
func netLocalGroupAddMember(group, username string) error {
	gp, err := windows.UTF16PtrFromString(group)
	if err != nil {
		return err
	}
	up, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return err
	}
	info := localGroupMembersInfo3{DomainAndName: up}
	r0, _, _ := procNetLocalGroupAddMembers.Call(
		0,
		uintptr(unsafe.Pointer(gp)),
		3,
		uintptr(unsafe.Pointer(&info)),
		1,
	)
	if r0 != 0 && syscall.Errno(r0) != errorMemberInAlias {
		return mapNetUserError(syscall.Errno(r0))
	}
	return nil
}

// deleteProfile 按 SID 删除用户配置文件。
func deleteProfile(sidString string) error {
	sidp, err := windows.UTF16PtrFromString(sidString)
	if err != nil {
		return err
	}
	r1, _, e := procDeleteProfileW.Call(uintptr(unsafe.Pointer(sidp)), 0, 0)
	if r1 == 0 {
		return fmt.Errorf("DeleteProfile: %w", e)
	}
	return nil
}

// postMessage 向窗口投递消息，不阻塞等待处理。
func postMessage(hwnd windows.HWND, msg uint32, wparam, lparam uintptr) {
	procPostMessageW.Call(uintptr(hwnd), uintptr(msg), wparam, lparam)
}

// loadUserProfile 加载用户配置文件（首次登录会创建 Profile），避免再启动 cmd.exe。
func loadUserProfile(token windows.Token, username string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return 0, err
	}
	info := profileInfoW{
		Size:     uint32(unsafe.Sizeof(profileInfoW{})),
		Flags:    piNoUI,
		UserName: name,
	}
	r1, _, e := procLoadUserProfileW.Call(uintptr(token), uintptr(unsafe.Pointer(&info)))
	if r1 == 0 {
		return 0, fmt.Errorf("LoadUserProfile: %w", e)
	}
	return info.Profile, nil
}

// unloadUserProfile 卸载已加载的用户配置文件。
func unloadUserProfile(token windows.Token, profile windows.Handle) error {
	r1, _, e := procUnloadUserProfile.Call(uintptr(token), uintptr(profile))
	if r1 == 0 {
		return fmt.Errorf("UnloadUserProfile: %w", e)
	}
	return nil
}

// processWindowStation 返回当前进程窗口站，调用方不要关闭该句柄。
func processWindowStation() (windows.Handle, error) {
	r1, _, e := procGetProcessWindowStation.Call()
	if r1 == 0 {
		return 0, fmt.Errorf("GetProcessWindowStation: %w", e)
	}
	return windows.Handle(r1), nil
}

// openDesktop 打开指定桌面。
func openDesktop(name string, access uint32) (windows.Handle, error) {
	np, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	r1, _, e := procOpenDesktopW.Call(uintptr(unsafe.Pointer(np)), 0, 0, uintptr(access))
	if r1 == 0 {
		return 0, fmt.Errorf("OpenDesktop: %w", e)
	}
	return windows.Handle(r1), nil
}

// closeDesktop 关闭桌面句柄。
func closeDesktop(h windows.Handle) {
	procCloseDesktop.Call(uintptr(h))
}

// mapNetUserError 将 NetUser* 状态码转成可读错误。
func mapNetUserError(e syscall.Errno) error {
	switch e {
	case nerrUserExists:
		return e
	case nerrUserNotFound:
		return e
	case nerrPasswordTooShort:
		return fmt.Errorf("password does not meet local policy: %w", e)
	case nerrBadUsername:
		return fmt.Errorf("invalid Windows username: %w", e)
	case 5:
		return fmt.Errorf("access denied (run as Administrator): %w", e)
	default:
		return fmt.Errorf("net user: %w", e)
	}
}

func isUserExists(err error) bool {
	return err == nerrUserExists
}

func isUserNotFound(err error) bool {
	return err == nerrUserNotFound
}
