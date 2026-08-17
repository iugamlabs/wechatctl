//go:build windows

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/paths"
)

// Write 在开始菜单中创建实例快捷方式。
func Write(layout paths.Layout, inst config.Instance) error {
	if err := os.MkdirAll(layout.ApplicationsDir, 0o755); err != nil {
		return err
	}
	bin, err := wxctlBinary()
	if err != nil {
		return err
	}
	return writeShortcut(
		layout.DesktopFile(inst.Name),
		bin,
		"start "+inst.Name+" --detach",
		"WeChat instance "+inst.Name,
		inst.WechatBin,
	)
}

// Remove 删除实例快捷方式。
func Remove(layout paths.Layout, name string) error {
	err := os.Remove(layout.DesktopFile(name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// writeShortcut 通过 PowerShell COM 生成 .lnk 文件。
func writeShortcut(lnk, target, args, desc, icon string) error {
	var b strings.Builder
	b.WriteString("$s = (New-Object -ComObject WScript.Shell).CreateShortcut(" + psQuote(lnk) + ")\n")
	b.WriteString("$s.TargetPath = " + psQuote(target) + "\n")
	b.WriteString("$s.Arguments = " + psQuote(args) + "\n")
	b.WriteString("$s.WorkingDirectory = " + psQuote(filepath.Dir(target)) + "\n")
	b.WriteString("$s.WindowStyle = 7\n")
	b.WriteString("$s.Description = " + psQuote(desc) + "\n")
	if icon != "" {
		b.WriteString("$s.IconLocation = " + psQuote(icon) + "\n")
	}
	b.WriteString("$s.Save()\n")
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", b.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create shortcut: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// psQuote 将字符串包装为 PowerShell 单引号字面量。
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
