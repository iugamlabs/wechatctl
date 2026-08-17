//go:build windows

package backend

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/windows"
)

// grantModifyName 按账户名授予目录及其子对象的修改权限。
func grantModifyName(path, username string) error {
	sid, _, _, err := windows.LookupSID("", username)
	if err != nil {
		return fmt.Errorf("lookup SID for %s: %w", username, err)
	}
	return grantModifySID(path, sid)
}

// grantModifySID 在现有 DACL 上追加一条可继承的修改权限 ACE。
func grantModifySID(path string, sid *windows.SID) error {
	if path == "" || sid == nil {
		return fmt.Errorf("grant ACL: empty path or SID")
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("GetNamedSecurityInfo %s: %w", path, err)
	}
	var old *windows.ACL
	if dacl, _, err := sd.DACL(); err == nil {
		old = dacl
	}

	var pinner runtime.Pinner
	pinner.Pin(sid)
	defer pinner.Unpin()

	entries := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.FILE_GENERIC_READ | windows.FILE_GENERIC_WRITE | windows.FILE_GENERIC_EXECUTE | windows.DELETE,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
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
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.UNPROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return fmt.Errorf("SetNamedSecurityInfo %s: %w", path, err)
	}
	return nil
}
