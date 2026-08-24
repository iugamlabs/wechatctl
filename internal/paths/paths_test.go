package paths

import (
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home := filepath.Join("/", "home", "deali")
	cases := map[string]string{
		"":        "",
		"~":       home,
		"~/docs":  filepath.Join(home, "docs"),
		`~\docs`:  filepath.Join(home, "docs"),
		"/data":    "/data",
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
