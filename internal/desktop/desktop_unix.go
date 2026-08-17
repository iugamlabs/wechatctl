//go:build !windows

package desktop

import (
	"fmt"
	"os"
	"strings"

	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/paths"
)

// Write 创建或更新实例的 .desktop 启动器。
func Write(layout paths.Layout, inst config.Instance) error {
	if err := os.MkdirAll(layout.ApplicationsDir, 0o755); err != nil {
		return err
	}
	bin, err := wxctlBinary()
	if err != nil {
		return err
	}
	content := formatDesktop(bin, inst)
	path := layout.DesktopFile(inst.Name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Remove 删除实例的 .desktop 文件。
func Remove(layout paths.Layout, name string) error {
	err := os.Remove(layout.DesktopFile(name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func formatDesktop(bin string, inst config.Instance) string {
	name := inst.DisplayName()
	execLine := shellQuote(bin) + " start " + shellQuote(inst.Name)
	var b strings.Builder
	b.WriteString("[Desktop Entry]\n")
	b.WriteString("Version=1.0\n")
	b.WriteString("Type=Application\n")
	b.WriteString("Name=" + escapeDesktop(name) + "\n")
	b.WriteString("Comment=WeChat instance " + escapeDesktop(inst.Name) + "\n")
	b.WriteString("Exec=" + execLine + "\n")
	b.WriteString("Icon=wechat\n")
	b.WriteString("Terminal=false\n")
	b.WriteString("Categories=Network;InstantMessaging;\n")
	b.WriteString("StartupNotify=true\n")
	b.WriteString(fmt.Sprintf("X-Wxctl-Instance=%s\n", inst.Name))
	return b.String()
}

func escapeDesktop(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func shellQuote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"'\\$`") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
