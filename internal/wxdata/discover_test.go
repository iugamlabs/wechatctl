package wxdata

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiscoverDBDirSkipsAllUsersAndPicksNewest(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "xwechat_files")

	mk := func(wxid string, msgMtime time.Time) {
		base := filepath.Join(root, wxid, "db_storage")
		if err := os.MkdirAll(filepath.Join(base, "message"), 0o755); err != nil {
			t.Fatal(err)
		}
		touch(filepath.Join(base, "message"), msgMtime)
	}

	// all_users 含 db_storage 布局也应跳过。
	if err := os.MkdirAll(filepath.Join(root, "all_users", "db_storage", "session"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "WMPF", "db_storage", "contact"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldT := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	newT := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	mk("wxid_a_1", oldT)
	mk("wxid_b_2", newT)

	got, err := DiscoverDBDir(home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "xwechat_files", "wxid_b_2", "db_storage")
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestDiscoverDBDirMissingRoot(t *testing.T) {
	_, err := DiscoverDBDir(t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}

func touch(path string, mt time.Time) {
	if err := os.MkdirAll(path, 0o755); err != nil {
		panic(err)
	}
	if err := os.Chtimes(path, mt, mt); err != nil {
		panic(err)
	}
}
