//go:build windows

package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindWeixinVersionDir(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "4.0.0.1")
	newDir := filepath.Join(root, "4.1.7.33")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "Weixin.dll"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "Weixin.dll"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := findWeixinVersionDir(root)
	if got != newDir {
		t.Fatalf("got %q, want %q", got, newDir)
	}
}

func TestParseDottedVersion(t *testing.T) {
	parts, ok := parseDottedVersion("4.1.7.33")
	if !ok || len(parts) != 4 || parts[3] != 33 {
		t.Fatalf("parse failed: %v %v", parts, ok)
	}
	if _, ok := parseDottedVersion("config"); ok {
		t.Fatal("non-version name should not parse")
	}
}

func TestPrependPath(t *testing.T) {
	base := appendEnvUTF16(nil, "PATH=C:\\Windows\\System32", "FOO=bar")
	out := prependPath(base, `C:\Program Files\Tencent\Weixin\4.1.7.33`, `C:\Program Files\Tencent\Weixin`)
	entries := splitEnvBlock(&out[0])
	var path string
	for _, e := range entries {
		if len(e) >= 5 && e[:5] == "PATH=" {
			path = e[5:]
		}
	}
	wantPrefix := `C:\Program Files\Tencent\Weixin\4.1.7.33;C:\Program Files\Tencent\Weixin;`
	if len(path) < len(wantPrefix) || path[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("PATH=%q, want prefix %q", path, wantPrefix)
	}
}
