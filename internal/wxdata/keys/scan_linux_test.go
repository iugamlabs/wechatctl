package keys

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/star-plan/wechatctl/internal/wxdata/crypto"
)

func TestScanMemoryForKeys64And96(t *testing.T) {
	encKey := bytes.Repeat([]byte{0x11}, crypto.KeySize)
	saltA := bytes.Repeat([]byte{0x22}, crypto.SaltSize)
	saltB := bytes.Repeat([]byte{0x33}, crypto.SaltSize)
	iv := bytes.Repeat([]byte{0x44}, 16)
	body := bytes.Repeat([]byte{'P'}, crypto.PageSize-crypto.ReserveSize-crypto.SaltSize)
	pageA := encryptPage1(encKey, saltA, iv, body)
	pageB := encryptPage1(encKey, saltB, iv, body)

	files := []DBFile{
		{Rel: "session/session.db", Salt: hex.EncodeToString(saltA), Page1: pageA, Size: crypto.PageSize},
		{Rel: "contact/contact.db", Salt: hex.EncodeToString(saltB), Page1: pageB, Size: crypto.PageSize},
	}
	saltToDBs := map[string][]string{
		files[0].Salt: {files[0].Rel},
		files[1].Salt: {files[1].Rel},
	}

	encHex := hex.EncodeToString(encKey)
	saltAHex := hex.EncodeToString(saltA)
	mem := []byte("pad x'" + encHex + "' more x'" + encHex + saltAHex + "'")

	keyMap := map[string]string{}
	remaining := remainingSet(saltToDBs)
	n := ScanMemoryForKeys(mem, 0x1000, 99, files, saltToDBs, keyMap, remaining, nil)
	if n != 2 {
		t.Fatalf("matches=%d want 2", n)
	}
	if keyMap[files[0].Salt] != encHex {
		t.Fatalf("salt A not found: %v", keyMap)
	}
	// 64-hex 会对 remaining 再试，B 也可能已被 64-hex 命中
	if _, ok := keyMap[files[1].Salt]; !ok {
		CrossVerifyKeys(files, saltToDBs, keyMap, nil)
	}
	if keyMap[files[1].Salt] != encHex {
		t.Fatalf("salt B not found after cross-verify: %v", keyMap)
	}
}

func TestCrossVerifyKeysFillsMissingSalt(t *testing.T) {
	encKey := bytes.Repeat([]byte{0xaa}, crypto.KeySize)
	saltA := bytes.Repeat([]byte{0x01}, crypto.SaltSize)
	saltB := bytes.Repeat([]byte{0x02}, crypto.SaltSize)
	iv := bytes.Repeat([]byte{0x03}, 16)
	body := bytes.Repeat([]byte{'Q'}, crypto.PageSize-crypto.ReserveSize-crypto.SaltSize)
	pageA := encryptPage1(encKey, saltA, iv, body)
	pageB := encryptPage1(encKey, saltB, iv, body)
	files := []DBFile{
		{Rel: "a.db", Salt: hex.EncodeToString(saltA), Page1: pageA},
		{Rel: "b.db", Salt: hex.EncodeToString(saltB), Page1: pageB},
	}
	saltToDBs := map[string][]string{files[0].Salt: {"a.db"}, files[1].Salt: {"b.db"}}
	keyMap := map[string]string{files[0].Salt: hex.EncodeToString(encKey)}
	CrossVerifyKeys(files, saltToDBs, keyMap, nil)
	if keyMap[files[1].Salt] != hex.EncodeToString(encKey) {
		t.Fatalf("cross-verify missed B: %v", keyMap)
	}
}

func TestMissingRequired(t *testing.T) {
	files := []DBFile{
		{Rel: "session/session.db"},
		{Rel: "contact/contact.db"},
		{Rel: "message/message_0.db"},
		{Rel: "message/message_fts.db"},
	}
	entries := map[string]KeyInfo{
		"session/session.db": {EncKey: "aa"},
	}
	miss := MissingRequired(files, entries)
	if len(miss) < 2 {
		t.Fatalf("miss=%v", miss)
	}
	entries["contact/contact.db"] = KeyInfo{EncKey: "bb"}
	entries["message/message_0.db"] = KeyInfo{EncKey: "cc"}
	if miss := MissingRequired(files, entries); len(miss) != 0 {
		t.Fatalf("should be complete: %v", miss)
	}
}

func TestListInstanceWeChatPIDsFailClosed(t *testing.T) {
	root := t.TempDir()
	prev := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = prev })

	mk := func(pid int, comm, instance string, rss int, environ bool) {
		dir := filepath.Join(root, strconv.Itoa(pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "statm"), []byte("1 "+strconv.Itoa(rss)+" 0\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/usr/bin/"+comm, filepath.Join(dir, "exe")); err != nil {
			t.Fatal(err)
		}
		if environ {
			data := []byte("HOME=/tmp\x00WXCTL_INSTANCE=" + instance + "\x00")
			if err := os.WriteFile(filepath.Join(dir, "environ"), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	mk(1001, "wechat", "work", 50, true)
	mk(1002, "wechat", "tk", 80, true)
	mk(1003, "wechat", "work", 10, false) // no environ → skip
	mk(1004, "python", "work", 90, true)
	mk(1005, "wechatappex", "work", 200, true)
	mk(1006, "wechat", "work2", 300, true)

	got := ListInstanceWeChatPIDs("work")
	if len(got) != 2 || got[0] != 1005 || got[1] != 1001 {
		t.Fatalf("got %v want [1005 1001] (RSS desc, fail-closed)", got)
	}
}

func TestIsWeChatProcessSkipsSelf(t *testing.T) {
	if IsWeChatProcess(os.Getpid()) {
		t.Fatal("self pid must be skipped")
	}
}

func TestHasPtraceRoot(t *testing.T) {
	prev := geteuidFn
	geteuidFn = func() int { return 0 }
	t.Cleanup(func() { geteuidFn = prev })
	if !HasPtrace() {
		t.Fatal("euid 0 must pass")
	}
}
