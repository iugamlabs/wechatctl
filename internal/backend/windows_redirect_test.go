//go:build windows

package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/paths"
)

func TestWindowsRedirectCreateAndEnvironment(t *testing.T) {
	root := t.TempDir()
	b := windowsRedirect{
		layout: paths.Layout{RunDir: filepath.Join(root, "run")},
		cfg:    config.Config{ProfilesRoot: filepath.Join(root, "instances")},
	}
	inst := config.Instance{Name: "work"}

	if _, err := b.Create(inst); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, dir := range []string{b.roamingDir(inst), b.localDir(inst), b.documentsDir(inst)} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("instance directory %q was not created: %v", dir, err)
		}
	}

	env := b.environment(inst)
	if got := windowsEnvValue(env, "APPDATA"); got != b.roamingDir(inst) {
		t.Fatalf("APPDATA=%q, want %q", got, b.roamingDir(inst))
	}
	if got := windowsEnvValue(env, "LOCALAPPDATA"); got != b.localDir(inst) {
		t.Fatalf("LOCALAPPDATA=%q, want %q", got, b.localDir(inst))
	}
	if got := windowsEnvValue(env, paths.InstanceEnvKey); got != inst.Name {
		t.Fatalf("%s=%q, want %q", paths.InstanceEnvKey, got, inst.Name)
	}
}

func TestSetWindowsEnvReplacesCaseInsensitively(t *testing.T) {
	env := setWindowsEnv([]string{"AppData=old", "OTHER=value"}, "APPDATA", "new")
	if got := windowsEnvValue(env, "appdata"); got != "new" {
		t.Fatalf("APPDATA=%q, want new", got)
	}
	count := 0
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, "APPDATA") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("APPDATA entries=%d, want 1", count)
	}
}

// windowsEnvValue is deliberately case-insensitive because Windows environment
// variable names are case-insensitive even when Go represents them as strings.
func windowsEnvValue(env []string, want string) string {
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, want) {
			return value
		}
	}
	return ""
}
