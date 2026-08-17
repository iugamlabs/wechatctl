//go:build windows

package backend

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/windows"
)

// grantInteractiveDesktop 授予用户访问当前交互窗口站和默认桌面的权限。
// 缺少该权限时，以该用户启动的进程会在 user32 初始化阶段以 0xC0000142 失败。
func grantInteractiveDesktop(sid *windows.SID) error {
	if sid == nil {
		return fmt.Errorf("grant desktop: nil SID")
	}
	winsta, err := processWindowStation()
	if err != nil {
		return err
	}
	if err := grantUserObjectAccess(winsta, sid, winstaAllAccess, windows.SUB_CONTAINERS_ONLY_INHERIT); err != nil {
		return fmt.Errorf("grant window station: %w", err)
	}
	desk, err := openDesktop("Default", windows.READ_CONTROL|windows.WRITE_DAC)
	if err != nil {
		return err
	}
	defer closeDesktop(desk)
	if err := grantUserObjectAccess(desk, sid, desktopAllAccess, 0); err != nil {
		return fmt.Errorf("grant desktop: %w", err)
	}
	return nil
}

// grantUserObjectAccess 在窗口站或桌面的 DACL 上追加一条允许 ACE。
func grantUserObjectAccess(handle windows.Handle, sid *windows.SID, access windows.ACCESS_MASK, inherit uint32) error {
	sd, err := windows.GetSecurityInfo(handle, windows.SE_WINDOW_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("GetSecurityInfo: %w", err)
	}
	var old *windows.ACL
	if dacl, _, err := sd.DACL(); err == nil {
		old = dacl
	}
	var pinner runtime.Pinner
	pinner.Pin(sid)
	defer pinner.Unpin()
	entries := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: access,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inherit,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}}
	acl, err := windows.ACLFromEntries(entries, old)
	if err != nil {
		return fmt.Errorf("ACLFromEntries: %w", err)
	}
	if err := windows.SetSecurityInfo(handle, windows.SE_WINDOW_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		return fmt.Errorf("SetSecurityInfo: %w", err)
	}
	return nil
}

// enableProfilePrivileges 启用 LoadUserProfile 所需的备份/还原特权。
func enableProfilePrivileges() {
	token := windows.GetCurrentProcessToken()
	for _, name := range []string{"SeRestorePrivilege", "SeBackupPrivilege"} {
		np, err := windows.UTF16PtrFromString(name)
		if err != nil {
			continue
		}
		var luid windows.LUID
		if err := windows.LookupPrivilegeValue(nil, np, &luid); err != nil {
			continue
		}
		tp := windows.Tokenprivileges{
			PrivilegeCount: 1,
			Privileges: [1]windows.LUIDAndAttributes{{
				Luid:       luid,
				Attributes: windows.SE_PRIVILEGE_ENABLED,
			}},
		}
		_ = windows.AdjustTokenPrivileges(token, false, &tp, 0, nil, nil)
	}
}
