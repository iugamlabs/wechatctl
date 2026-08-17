//go:build windows

package backend

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// createOrResetLocalUser 创建 wxctl 管理的本地用户；若已存在则重置密码。
func createOrResetLocalUser(username, password, comment string) error {
	err := netUserAdd(username, password, comment)
	if err != nil {
		if !isUserExists(err) {
			return fmt.Errorf("create local user %s: %w", username, err)
		}
		if !IsManagedUsername(username) {
			return fmt.Errorf("Windows user %q already exists and is not a wxctl-managed account", username)
		}
		if err := netUserSetPassword(username, password); err != nil {
			return fmt.Errorf("reset password for existing user %s: %w", username, err)
		}
	}
	if err := addUserToUsersGroup(username); err != nil {
		return err
	}
	return nil
}

// addUserToUsersGroup 把本地用户加入 Users 组，否则无法读取 Program Files 中的微信 DLL。
func addUserToUsersGroup(username string) error {
	group, err := builtinUsersGroupName()
	if err != nil {
		return err
	}
	if err := netLocalGroupAddMember(group, username); err != nil {
		return fmt.Errorf("add %s to %s: %w", username, group, err)
	}
	return nil
}

// builtinUsersGroupName 解析 BUILTIN\Users 的本地组名（中文系统可能显示为“用户”）。
func builtinUsersGroupName() (string, error) {
	sid, err := windows.CreateWellKnownSid(windows.WinBuiltinUsersSid)
	if err != nil {
		return "Users", nil
	}
	account, _, _, err := sid.LookupAccount("")
	if err != nil || account == "" {
		return "Users", nil
	}
	return account, nil
}

// withLoadedProfile 登录并加载用户配置文件，在回调期间 hive 可用，然后卸载。
func withLoadedProfile(username, password string, fn func(profileDir string) error) error {
	enableProfilePrivileges()
	token, err := logonUser(username, ".", password)
	if err != nil {
		return fmt.Errorf("logon for profile: %w", err)
	}
	defer token.Close()

	handle, err := loadUserProfile(token, username)
	if err != nil {
		return err
	}
	defer func() { _ = unloadUserProfile(token, handle) }()

	profileDir, err := token.GetUserProfileDirectory()
	if err != nil || profileDir == "" {
		profileDir = fallbackProfileDir(username)
	}
	return fn(profileDir)
}

// fallbackProfileDir 按约定返回 %SystemDrive%\Users\<username>。
func fallbackProfileDir(username string) string {
	root := os.Getenv("SYSTEMDRIVE")
	if root == "" {
		root = "C:"
	}
	return filepath.Join(root+`\Users`, username)
}

// deleteLocalUserAndProfile 删除本地用户及其 Profile 目录。
func deleteLocalUserAndProfile(username string) error {
	if !IsManagedUsername(username) {
		return fmt.Errorf("refusing to delete unmanaged Windows user %q", username)
	}
	var sidStr string
	if sid, _, _, err := windows.LookupSID("", username); err == nil {
		sidStr = sid.String()
	}
	if err := netUserDel(username); err != nil && !isUserNotFound(err) {
		return fmt.Errorf("delete user %s: %w", username, err)
	}
	if sidStr != "" {
		_ = deleteProfile(sidStr)
	}
	_ = os.RemoveAll(fallbackProfileDir(username))
	return nil
}

// currentUserSID 返回当前进程令牌中的用户 SID。
func currentUserSID() (*windows.SID, error) {
	token := windows.GetCurrentProcessToken()
	tu, err := token.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return tu.User.Sid.Copy()
}
