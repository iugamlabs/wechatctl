package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// wxctlBinary 定位当前 wxctl 可执行文件，供启动器引用。
func wxctlBinary() (string, error) {
	if exe, err := os.Executable(); err == nil {
		if resolved, err2 := filepath.EvalSymlinks(exe); err2 == nil {
			return resolved, nil
		}
		return exe, nil
	}
	if p, err := exec.LookPath("wxctl"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("cannot locate wxctl binary")
}
