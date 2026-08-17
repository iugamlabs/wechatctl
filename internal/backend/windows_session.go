//go:build windows

package backend

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// interactiveHiveKeys 需要复制到隔离用户的交互会话相关注册表。
var interactiveHiveKeys = []string{
	`Software\Microsoft\Windows\CurrentVersion\Themes`,
	`Software\Microsoft\Windows\CurrentVersion\ThemeManager`,
	`Software\Microsoft\Windows\DWM`,
	`Software\Microsoft\CTF`,
	`Software\Microsoft\Input`,
	`Software\Microsoft\InputMethod`,
	`Keyboard Layout`,
	`Control Panel\Desktop`,
	`Control Panel\International`,
}

// seedInteractiveHive 把当前用户的主题、DPI 和输入法配置写入隔离用户 hive。
// 缺少这些项时，以该用户启动的 GUI 会变成经典灰边框，并且无法使用 IME。
func seedInteractiveHive(username string) error {
	sid, _, _, err := windows.LookupSID("", username)
	if err != nil {
		return err
	}
	dstRoot := sid.String()
	for _, path := range interactiveHiveKeys {
		_ = copyRegistryTree(registry.CURRENT_USER, path, registry.USERS, dstRoot+`\`+path)
	}
	return ensureThemeActive(dstRoot)
}

// ensureThemeActive 强制隔离用户启用视觉样式，避免经典灰色非客户区边框。
func ensureThemeActive(sidRoot string) error {
	k, _, err := registry.CreateKey(registry.USERS, sidRoot+`\Software\Microsoft\Windows\CurrentVersion\ThemeManager`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("ThemeActive", "1")
}

// copyRegistryTree 递归复制注册表树；源不存在时忽略。
func copyRegistryTree(srcRoot registry.Key, srcPath string, dstRoot registry.Key, dstPath string) error {
	src, err := registry.OpenKey(srcRoot, srcPath, registry.READ)
	if err != nil {
		return nil
	}
	defer src.Close()
	dst, _, err := registry.CreateKey(dstRoot, dstPath, registry.WRITE)
	if err != nil {
		return err
	}
	defer dst.Close()

	names, err := src.ReadValueNames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		_ = copyRegistryValue(src, dst, name)
	}
	subs, err := src.ReadSubKeyNames(-1)
	if err != nil {
		return err
	}
	for _, sub := range subs {
		if err := copyRegistryTree(srcRoot, srcPath+`\`+sub, dstRoot, dstPath+`\`+sub); err != nil {
			return err
		}
	}
	return nil
}

// copyRegistryValue 按类型复制单个注册表值。
func copyRegistryValue(src, dst registry.Key, name string) error {
	_, typ, err := src.GetValue(name, nil)
	if err != nil {
		return err
	}
	switch typ {
	case registry.SZ:
		s, _, err := src.GetStringValue(name)
		if err != nil {
			return err
		}
		return dst.SetStringValue(name, s)
	case registry.EXPAND_SZ:
		s, _, err := src.GetStringValue(name)
		if err != nil {
			return err
		}
		return dst.SetExpandStringValue(name, s)
	case registry.DWORD:
		n, _, err := src.GetIntegerValue(name)
		if err != nil {
			return err
		}
		return dst.SetDWordValue(name, uint32(n))
	case registry.QWORD:
		n, _, err := src.GetIntegerValue(name)
		if err != nil {
			return err
		}
		return dst.SetQWordValue(name, n)
	case registry.BINARY:
		b, _, err := src.GetBinaryValue(name)
		if err != nil {
			return err
		}
		return dst.SetBinaryValue(name, b)
	case registry.MULTI_SZ:
		ss, _, err := src.GetStringsValue(name)
		if err != nil {
			return err
		}
		return dst.SetStringsValue(name, ss)
	default:
		return nil
	}
}

// ensureCtfmon 以隔离用户身份在当前桌面启动输入法框架进程。
func ensureCtfmon(username, password string) error {
	ctf := filepath.Join(os.Getenv("SystemRoot"), "System32", "ctfmon.exe")
	if _, err := os.Stat(ctf); err != nil {
		return fmt.Errorf("ctfmon not found: %w", err)
	}
	env, err := userEnvironment(username, password, "")
	if err != nil {
		env = nil
	}
	cmdLine := windows.ComposeCommandLine([]string{ctf})
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT)
	pi, err := createProcessWithLogon(username, ".", password, ctf, cmdLine, env, "", flags, true)
	if err != nil {
		return fmt.Errorf("start ctfmon as %s: %w", username, err)
	}
	windows.CloseHandle(pi.Thread)
	windows.CloseHandle(pi.Process)
	return nil
}
