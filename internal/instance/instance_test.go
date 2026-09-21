package instance

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/star-plan/wechatctl/internal/config"
	"github.com/star-plan/wechatctl/internal/paths"
)

func TestRemovePurgeDeletesWxdataWhenHomeMissing(t *testing.T) {
	root := t.TempDir()
	layout := paths.Layout{
		Home:          root,
		ConfigDir:     filepath.Join(root, "config"),
		InstancesFile: filepath.Join(root, "config", "instances.toml"),
		DataDir:       filepath.Join(root, "data"),
		ProfilesRoot:  filepath.Join(root, "data", "instances"),
		RunDir:        filepath.Join(root, "data", "run"),
	}
	if err := os.MkdirAll(layout.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := config.Registry{Instances: []config.Instance{{Name: "work", Backend: "linux-home"}}}
	if err := config.SaveRegistry(layout, reg); err != nil {
		t.Fatal(err)
	}

	wxdata := layout.WxdataDir("work")
	if err := os.MkdirAll(wxdata, 0o700); err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(wxdata, "all_keys.json")
	if err := os.WriteFile(keyFile, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := Manager{Layout: layout, Config: config.Default(layout)}
	var out bytes.Buffer
	if err := mgr.Remove("work", RemoveOptions{Purge: true, Yes: true, Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wxdata); !os.IsNotExist(err) {
		t.Fatalf("wxdata still exists: %v", err)
	}
}

func TestRemovePurgeConfirmMentionsWxdata(t *testing.T) {
	root := t.TempDir()
	layout := paths.Layout{
		Home:          root,
		ConfigDir:     filepath.Join(root, "config"),
		InstancesFile: filepath.Join(root, "config", "instances.toml"),
		DataDir:       filepath.Join(root, "data"),
		ProfilesRoot:  filepath.Join(root, "data", "instances"),
		RunDir:        filepath.Join(root, "data", "run"),
	}
	if err := os.MkdirAll(layout.ConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := config.Registry{Instances: []config.Instance{{Name: "work", Backend: "linux-home"}}}
	if err := config.SaveRegistry(layout, reg); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(layout.ProfilesRoot, "work")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	wxdata := layout.WxdataDir("work")
	if err := os.MkdirAll(wxdata, 0o700); err != nil {
		t.Fatal(err)
	}

	mgr := Manager{Layout: layout, Config: config.Default(layout)}
	var out bytes.Buffer
	var in bytes.Buffer
	in.WriteString("work\n")

	if err := mgr.Remove("work", RemoveOptions{
		Purge:  true,
		Stdin:  &in,
		Stdout: &out,
	}); err != nil {
		t.Fatal(err)
	}
	msg := out.String()
	if !strings.Contains(msg, home) {
		t.Fatalf("confirm missing home %q in:\n%s", home, msg)
	}
	if !strings.Contains(msg, wxdata) {
		t.Fatalf("confirm missing wxdata %q in:\n%s", wxdata, msg)
	}
	if !strings.Contains(msg, "wxdata") {
		t.Fatalf("confirm should mention wxdata:\n%s", msg)
	}
}
