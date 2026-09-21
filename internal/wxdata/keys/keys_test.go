package keys

import (
	"path/filepath"
	"testing"
)

func TestGetKeyInfoRejectsDotDot(t *testing.T) {
	m := map[string]KeyInfo{"session/session.db": {EncKey: "a"}}
	if _, ok := GetKeyInfo(m, "../session.db"); ok {
		t.Fatal("expected reject")
	}
}

func TestGetKeyInfoPathVariants(t *testing.T) {
	m := map[string]KeyInfo{
		"session/session.db": {EncKey: "deadbeef"},
	}
	for _, rel := range []string{
		"session/session.db",
		"session\\session.db",
		filepath.Join("session", "session.db"),
	} {
		info, ok := GetKeyInfo(m, rel)
		if !ok || info.EncKey != "deadbeef" {
			t.Fatalf("lookup %q failed", rel)
		}
	}
}

func TestClassifyHexLengths(t *testing.T) {
	hex64 := makeHex(64)
	enc, salt, ok := ClassifyHex(hex64)
	if !ok || enc != hex64 || salt != "" {
		t.Fatalf("64: enc=%q salt=%q ok=%v", enc, salt, ok)
	}

	hex96 := makeHex(96)
	enc, salt, ok = ClassifyHex(hex96)
	if !ok || len(enc) != 64 || len(salt) != 32 {
		t.Fatalf("96: enc len=%d salt len=%d", len(enc), len(salt))
	}

	hex100 := makeHex(100)
	enc, salt, ok = ClassifyHex(hex100)
	if !ok || len(enc) != 64 || len(salt) != 32 {
		t.Fatalf("100: enc len=%d salt len=%d", len(enc), len(salt))
	}

	if _, _, ok := ClassifyHex(makeHex(63)); ok {
		t.Fatal("63 should fail")
	}
}

func TestHexMemoryRE(t *testing.T) {
	m := HexMemoryRE.FindStringSubmatch("x'0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef'")
	if m == nil || len(m[1]) != 64 {
		t.Fatalf("unexpected match: %v", m)
	}
}

func TestBuildEntriesAndMissingRequiredPattern(t *testing.T) {
	files := []DBFile{
		{Rel: "session/session.db", Salt: "aa", Size: 1024 * 1024},
		{Rel: "sns/sns.db", Salt: "bb", Size: 2048 * 1024},
	}
	entries, missing := BuildEntries(files, map[string]string{"aa": "deadbeef"})
	if entries["session/session.db"].EncKey != "deadbeef" || len(missing) != 1 || missing[0] != "sns/sns.db" {
		t.Fatalf("entries=%v missing=%v", entries, missing)
	}
	if miss := MissingRequired(files, entries); len(miss) < 2 {
		t.Fatalf("want contact + message missing, got %v", miss)
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "all_keys.json")
	entries := map[string]KeyInfo{
		"session/session.db": {EncKey: "aa", Salt: "bb", SizeMB: 1.2},
	}
	dbDir := "/tmp/db_storage"
	if err := Save(path, dbDir, entries); err != nil {
		t.Fatal(err)
	}
	gotDir, got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != dbDir || got["session/session.db"].EncKey != "aa" {
		t.Fatalf("load mismatch dir=%q keys=%v", gotDir, got)
	}
}

func makeHex(n int) string {
	const digits = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = digits[i%16]
	}
	return string(b)
}
