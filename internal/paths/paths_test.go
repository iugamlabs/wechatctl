package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home := filepath.Join("/", "home", "deali")
	cases := map[string]string{
		"":       "",
		"~":      home,
		"~/docs": filepath.Join(home, "docs"),
		`~\docs`: filepath.Join(home, "docs"),
		"/data":  "/data",
	}
	for in, want := range cases {
		got := ExpandPath(home, in)
		if got != want {
			t.Fatalf("ExpandPath(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestDesktopFileExt(t *testing.T) {
	l := Layout{ApplicationsDir: "apps"}
	got := l.DesktopFile("work")
	want := filepath.Join("apps", DesktopPrefix+"work"+DesktopExt)
	if got != want {
		t.Fatalf("DesktopFile=%q, want %q", got, want)
	}
}

func TestWxdataDir(t *testing.T) {
	l := Layout{DataDir: filepath.Join("/data", "wxctl")}
	got := l.WxdataDir("work")
	want := filepath.Join("/data", "wxctl", "wxdata", "work")
	if got != want {
		t.Fatalf("WxdataDir=%q, want %q", got, want)
	}
}

func withHomeHooks(t *testing.T, euid int, lookup func(string) (string, error)) {
	prevEuid := euidFn
	prevLookup := lookupHomeFn
	euidFn = func() int { return euid }
	if lookup != nil {
		lookupHomeFn = lookup
	}
	t.Cleanup(func() {
		euidFn = prevEuid
		lookupHomeFn = prevLookup
	})
}

func TestRealHomeNonRootIgnoresSudoUser(t *testing.T) {
	withHomeHooks(t, 1000, nil)
	t.Setenv("WXCTL_REAL_HOME", "")
	t.Setenv("SUDO_USER", "someone")
	t.Setenv("HOME", t.TempDir())

	got, err := realHome()
	if err != nil {
		t.Fatal(err)
	}
	want := os.Getenv("HOME")
	if got != want {
		t.Fatalf("realHome=%q, want %q (SUDO_USER ignored when euid!=0)", got, want)
	}
}

func TestRealHomeRootUsesSudoUserLookup(t *testing.T) {
	withHomeHooks(t, 0, func(username string) (string, error) {
		if username != "deali" {
			t.Fatalf("lookup username=%q", username)
		}
		return "/home/deali", nil
	})
	t.Setenv("WXCTL_REAL_HOME", "")
	t.Setenv("SUDO_USER", "deali")
	t.Setenv("HOME", "/root")

	got, err := realHome()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/home/deali" {
		t.Fatalf("realHome=%q, want /home/deali", got)
	}
}

func TestRealHomeWxctlRealHomeWins(t *testing.T) {
	withHomeHooks(t, 0, func(string) (string, error) {
		return "/home/deali", nil
	})
	t.Setenv("WXCTL_REAL_HOME", "/override/home")
	t.Setenv("SUDO_USER", "deali")

	got, err := realHome()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/override/home" {
		t.Fatalf("realHome=%q, want WXCTL_REAL_HOME", got)
	}
}

func TestDefaultLayoutRootIgnoresXDGEnv(t *testing.T) {
	withHomeHooks(t, 0, func(string) (string, error) {
		return "/home/deali", nil
	})
	t.Setenv("WXCTL_REAL_HOME", "")
	t.Setenv("SUDO_USER", "deali")
	t.Setenv("XDG_CONFIG_HOME", "/root/.config")
	t.Setenv("XDG_DATA_HOME", "/root/.local/share")
	t.Setenv("HOME", "/root")

	layout, err := DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	wantConfig := filepath.Join("/home/deali", ".config", AppName)
	if layout.ConfigDir != wantConfig {
		t.Fatalf("ConfigDir=%q, want %q", layout.ConfigDir, wantConfig)
	}
	wantData := filepath.Join("/home/deali", ".local", "share", AppName)
	if layout.DataDir != wantData {
		t.Fatalf("DataDir=%q, want %q", layout.DataDir, wantData)
	}
}

func TestDefaultLayoutNonRootUsesXDGEnv(t *testing.T) {
	withHomeHooks(t, 1000, nil)
	tmp := t.TempDir()
	t.Setenv("WXCTL_REAL_HOME", tmp)
	t.Setenv("SUDO_USER", "")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg-config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "xdg-data"))

	layout, err := DefaultLayout()
	if err != nil {
		t.Fatal(err)
	}
	if layout.ConfigDir != filepath.Join(tmp, "xdg-config", AppName) {
		t.Fatalf("ConfigDir=%q", layout.ConfigDir)
	}
	if layout.DataDir != filepath.Join(tmp, "xdg-data", AppName) {
		t.Fatalf("DataDir=%q", layout.DataDir)
	}
}
