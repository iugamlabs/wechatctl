package wxdata

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/star-plan/wechatctl/internal/wxdata/crypto"
	"github.com/star-plan/wechatctl/internal/wxdata/keys"
)

func TestPersistKeysAtomicAndMode(t *testing.T) {
	dir := t.TempDir()
	entries := map[string]keys.KeyInfo{
		"session/session.db": {EncKey: "aa", Salt: "bb", SizeMB: 1.2},
	}
	if err := os.WriteFile(keys.PartialPath(keys.KeysPath(dir)), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PersistKeys(dir, "/db", entries, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keys.PartialPath(keys.KeysPath(dir))); !os.IsNotExist(err) {
		t.Fatal("success must remove .partial")
	}
	path := keys.KeysPath(dir)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("perm=%o", st.Mode().Perm())
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("tmp should be gone")
	}
	stored, got, err := keys.Load(path)
	if err != nil || stored != "/db" || got["session/session.db"].EncKey != "aa" {
		t.Fatalf("load stored=%q keys=%v err=%v", stored, got, err)
	}
}

func TestPersistKeysChownFailRemovesOfficial(t *testing.T) {
	dir := t.TempDir()
	prev := chownFn
	chownFn = func(string, int, int) error { return errors.New("chown denied") }
	t.Cleanup(func() { chownFn = prev })

	entries := map[string]keys.KeyInfo{"session/session.db": {EncKey: "aa"}}
	err := PersistKeys(dir, "/db", entries, &Owner{UID: 1000, GID: 1000})
	if err == nil {
		t.Fatal("expected chown error")
	}
	if _, err := os.Stat(keys.KeysPath(dir)); !os.IsNotExist(err) {
		t.Fatal("official all_keys.json must not remain after chown failure")
	}
}

func TestPersistKeysChownFailKeepsExistingOfficial(t *testing.T) {
	dir := t.TempDir()
	path := keys.KeysPath(dir)
	old := map[string]keys.KeyInfo{"session/session.db": {EncKey: "old", Salt: "ss"}}
	if err := keys.Save(path, "/old", old); err != nil {
		t.Fatal(err)
	}
	prev := chownFn
	chownFn = func(string, int, int) error { return errors.New("chown denied") }
	t.Cleanup(func() { chownFn = prev })

	err := PersistKeys(dir, "/db", map[string]keys.KeyInfo{"session/session.db": {EncKey: "new"}}, &Owner{UID: 1, GID: 1})
	if err == nil {
		t.Fatal("expected error")
	}
	stored, got, err := keys.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored != "/old" || got["session/session.db"].EncKey != "old" {
		t.Fatalf("existing official overwritten: dir=%q keys=%v", stored, got)
	}
}

func TestPersistKeysChownAfterRenameRemovesOfficial(t *testing.T) {
	dir := t.TempDir()
	prev := chownFn
	chownFn = func(path string, uid, gid int) error {
		if strings.HasSuffix(path, "all_keys.json") && !strings.Contains(path, ".tmp") && !strings.Contains(path, ".partial") {
			return errors.New("chown after rename")
		}
		return nil
	}
	t.Cleanup(func() { chownFn = prev })

	err := PersistKeys(dir, "/db", map[string]keys.KeyInfo{"session/session.db": {EncKey: "aa"}}, &Owner{UID: 1, GID: 1})
	if err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(keys.KeysPath(dir)); !os.IsNotExist(err) {
		t.Fatal("official file must be removed if post-rename chown fails")
	}
}

func TestPersistKeysChownsWxdataParent(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "wxdata", "tk")
	var got []string
	prev := chownFn
	chownFn = func(path string, uid, gid int) error {
		got = append(got, path)
		if uid != 1000 || gid != 1001 {
			t.Fatalf("uid/gid=%d/%d", uid, gid)
		}
		return nil
	}
	t.Cleanup(func() { chownFn = prev })

	err := PersistKeys(state, "/db", map[string]keys.KeyInfo{"session/session.db": {EncKey: "aa"}}, &Owner{UID: 1000, GID: 1001})
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "wxdata")
	seenParent, seenState := false, false
	for _, p := range got {
		if p == parent {
			seenParent = true
		}
		if p == state {
			seenState = true
		}
	}
	if !seenParent || !seenState {
		t.Fatalf("chown paths=%v want parent %q and state %q", got, parent, state)
	}
}

func TestEnsureOwnedChownsWxdataParentEvenIfJSONAlreadyOwned(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "wxdata", "tk")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := keys.Save(keys.KeysPath(state), "/db", map[string]keys.KeyInfo{"a": {EncKey: "x"}}); err != nil {
		t.Fatal(err)
	}
	var got []string
	prev := chownFn
	chownFn = func(path string, uid, gid int) error {
		got = append(got, path)
		return nil
	}
	t.Cleanup(func() { chownFn = prev })

	if err := EnsureOwned(state, &Owner{UID: os.Getuid(), GID: os.Getgid()}); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "wxdata")
	for _, p := range got {
		if p == parent {
			return
		}
	}
	t.Fatalf("skip path must still chown %q, got %v", parent, got)
}

func TestEnsureOwnedDemotesOnChownFail(t *testing.T) {
	dir := t.TempDir()
	path := keys.KeysPath(dir)
	if err := keys.Save(path, "/db", map[string]keys.KeyInfo{"a": {EncKey: "x"}}); err != nil {
		t.Fatal(err)
	}
	prev := chownFn
	chownFn = func(string, int, int) error { return errors.New("nope") }
	t.Cleanup(func() { chownFn = prev })

	if err := EnsureOwned(dir, &Owner{UID: 99999, GID: 99999}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("official should be demoted")
	}
	if _, err := os.Stat(keys.PartialPath(path)); err != nil {
		t.Fatal("expected .partial")
	}
}

func TestWritePartialDoesNotTouchOfficial(t *testing.T) {
	dir := t.TempDir()
	path := keys.KeysPath(dir)
	if err := keys.Save(path, "/db", map[string]keys.KeyInfo{"a": {EncKey: "old"}}); err != nil {
		t.Fatal(err)
	}
	WritePartial(dir, "/db", map[string]keys.KeyInfo{"a": {EncKey: "partial"}})
	_, got, err := keys.Load(path)
	if err != nil || got["a"].EncKey != "old" {
		t.Fatalf("official changed: %v %v", got, err)
	}
	if _, err := os.Stat(keys.PartialPath(path)); err != nil {
		t.Fatal(err)
	}
}

func TestValidateExistingKeys(t *testing.T) {
	dbDir := t.TempDir()
	encKey := bytes.Repeat([]byte{0x11}, crypto.KeySize)
	salt := bytes.Repeat([]byte{0x22}, crypto.SaltSize)
	iv := bytes.Repeat([]byte{0x33}, 16)
	body := bytes.Repeat([]byte{'P'}, crypto.PageSize-crypto.ReserveSize-crypto.SaltSize)
	page := encryptPage1Persist(encKey, salt, iv, body)

	writeDB := func(rel string) {
		p := filepath.Join(dbDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, page, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeDB("session/session.db")
	writeDB("contact/contact.db")
	writeDB("message/message_0.db")

	state := t.TempDir()
	path := keys.KeysPath(state)
	info := keys.KeyInfo{EncKey: hex.EncodeToString(encKey), Salt: hex.EncodeToString(salt), SizeMB: 0.1}
	entries := map[string]keys.KeyInfo{
		"session/session.db":   info,
		"contact/contact.db":   info,
		"message/message_0.db": info,
	}
	if err := keys.Save(path, dbDir, entries); err != nil {
		t.Fatal(err)
	}
	if err := ValidateExistingKeys(path, dbDir); err != nil {
		t.Fatal(err)
	}

	// 缺 _db_dir：另存无 metadata 的 JSON
	if err := os.WriteFile(path, []byte(`{"session/session.db":{"enc_key":"aa"}}\n`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateExistingKeys(path, dbDir); err == nil {
		t.Fatal("missing _db_dir must fail")
	}
}

func TestResolveOwnerNonRoot(t *testing.T) {
	prev := geteuid
	geteuid = func() int { return 1000 }
	t.Cleanup(func() { geteuid = prev })
	t.Setenv("SUDO_USER", "someone")
	t.Setenv("SUDO_UID", "1")
	t.Setenv("SUDO_GID", "1")
	o, err := ResolveOwner()
	if err != nil || o != nil {
		t.Fatalf("non-root must not chown: %v %v", o, err)
	}
}

func TestResolveOwnerRootUsesSudoUID(t *testing.T) {
	prev := geteuid
	geteuid = func() int { return 0 }
	t.Cleanup(func() { geteuid = prev })
	t.Setenv("SUDO_UID", "1000")
	t.Setenv("SUDO_GID", "1001")
	o, err := ResolveOwner()
	if err != nil || o == nil || o.UID != 1000 || o.GID != 1001 {
		t.Fatalf("got %+v err=%v", o, err)
	}
}

func encryptPage1Persist(encKey, salt, iv, body []byte) []byte {
	block, err := aes.NewCipher(encKey)
	if err != nil {
		panic(err)
	}
	page := make([]byte, crypto.PageSize)
	copy(page[:crypto.SaltSize], salt)
	copy(page[crypto.PageSize-crypto.ReserveSize:crypto.PageSize-crypto.ReserveSize+16], iv)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(page[crypto.SaltSize:crypto.PageSize-crypto.ReserveSize], body)
	macSalt := make([]byte, len(salt))
	for i, b := range salt {
		macSalt[i] = b ^ crypto.HMACXOR
	}
	macKey, err := pbkdf2.Key(sha512.New, string(encKey), macSalt, crypto.PBKDF2Iter, crypto.KeySize)
	if err != nil {
		panic(err)
	}
	m := hmac.New(sha512.New, macKey)
	m.Write(page[crypto.SaltSize : crypto.PageSize-crypto.ReserveSize+16])
	var pgno [4]byte
	binary.LittleEndian.PutUint32(pgno[:], 1)
	m.Write(pgno[:])
	copy(page[crypto.PageSize-64:], m.Sum(nil))
	return page
}
