//go:build windows

package backend

import (
	"slices"
	"testing"

	"golang.org/x/sys/windows"
)

func TestAppendEnvUTF16(t *testing.T) {
	raw, err := windows.UTF16FromString("FOO=bar")
	if err != nil {
		t.Fatal(err)
	}
	// FOO=bar\0 + extra trailing NUL to form an env block.
	block := append(raw, 0)
	got := splitEnvBlock(&block[0])
	if !slices.Equal(got, []string{"FOO=bar"}) {
		t.Fatalf("split = %#v", got)
	}
	out := appendEnvUTF16(&block[0], "WXCTL_INSTANCE=work")
	entries := splitEnvBlock(&out[0])
	if !slices.Contains(entries, "FOO=bar") || !slices.Contains(entries, "WXCTL_INSTANCE=work") {
		t.Fatalf("appended env = %#v", entries)
	}
}
