package cache

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/star-plan/wechatctl/internal/wxdata/errkind"
	"github.com/star-plan/wechatctl/internal/wxdata/crypto"
	"github.com/star-plan/wechatctl/internal/wxdata/keys"
)

func TestCacheHitSkipsRedecrypt(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, "db_storage")
	cacheDir := filepath.Join(dir, "wxdata", "cache")
	rel := "session/session.db"
	encPath := filepath.Join(dbDir, "session", "session.db")
	encKey, salt := testKeySalt()
	writeEncryptedDB(encPath, encKey, salt)

	keyMap := map[string]keys.KeyInfo{
		rel: {EncKey: hex.EncodeToString(encKey), Salt: hex.EncodeToString(salt)},
	}
	c, err := New(keyMap, dbDir, cacheDir)
	if err != nil {
		t.Fatal(err)
	}

	p1, err := c.Get(rel)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(encPath)
	if err != nil {
		t.Fatal(err)
	}
	mod := info.ModTime()

	p2, err := c.Get(rel)
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 {
		t.Fatalf("paths differ: %s vs %s", p1, p2)
	}

	raw, err := os.ReadFile(filepath.Join(cacheDir, "_mtimes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mt map[string]mtimeEntry
	if err := json.Unmarshal(raw, &mt); err != nil {
		t.Fatal(err)
	}
	ent := mt[rel]
	if ent.DBMT != mod.UnixNano() || ent.DBSize != info.Size() || ent.WalMT != 0 || ent.WalSize != 0 {
		t.Fatalf("mtime entry: %+v", ent)
	}
	if ent.Path != p1 || !strings.HasSuffix(ent.Path, ".db") {
		t.Fatalf("bad path %q", ent.Path)
	}

	st, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("cache dir perm %o", st.Mode().Perm())
	}
}

func TestCacheMtimeChangeRedecrypts(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, "db_storage")
	cacheDir := filepath.Join(dir, "cache")
	rel := "session/session.db"
	encPath := filepath.Join(dbDir, "session", "session.db")
	encKey, salt := testKeySalt()
	writeEncryptedDB(encPath, encKey, salt)

	keyMap := map[string]keys.KeyInfo{
		rel: {EncKey: hex.EncodeToString(encKey), Salt: hex.EncodeToString(salt)},
	}
	c, err := New(keyMap, dbDir, cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Get(rel)
	if err != nil {
		t.Fatal(err)
	}
	raw1, err := os.ReadFile(filepath.Join(cacheDir, "_mtimes.json"))
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)
	newT := time.Now().Add(time.Hour)
	if err := os.Chtimes(encPath, newT, newT); err != nil {
		t.Fatal(err)
	}
	_, err = c.Get(rel)
	if err != nil {
		t.Fatal(err)
	}
	raw2, err := os.ReadFile(filepath.Join(cacheDir, "_mtimes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(raw1, raw2) {
		t.Fatal("expected _mtimes.json update after source mtime change")
	}
}

func TestCacheSaltMismatch(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, "db_storage")
	rel := "session/session.db"
	encPath := filepath.Join(dbDir, "session", "session.db")
	encKey, salt := testKeySalt()
	writeEncryptedDB(encPath, encKey, salt)

	wrongSalt := bytes.Repeat([]byte{0x99}, crypto.SaltSize)
	keyMap := map[string]keys.KeyInfo{
		rel: {EncKey: hex.EncodeToString(encKey), Salt: hex.EncodeToString(wrongSalt)},
	}
	c, err := New(keyMap, dbDir, filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Get(rel)
	if !errors.Is(err, errkind.ErrNoKeys) {
		t.Fatalf("expected ErrNoKeys, got %v", err)
	}
}

func TestOpenPlainDSN(t *testing.T) {
	dir := t.TempDir()
	dbDir := filepath.Join(dir, "db_storage")
	rel := "session/session.db"
	encPath := filepath.Join(dbDir, "session", "session.db")
	encKey, salt := testKeySalt()
	writeEncryptedDB(encPath, encKey, salt)
	keyMap := map[string]keys.KeyInfo{
		rel: {EncKey: hex.EncodeToString(encKey), Salt: hex.EncodeToString(salt)},
	}
	c, err := New(keyMap, dbDir, filepath.Join(dir, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := c.Get(rel)
	if err != nil {
		t.Fatal(err)
	}

	u := url.URL{Scheme: "file", Path: filepath.ToSlash(plain)}
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	dsn := u.String()
	if strings.Contains(dsn, "_pragma=") || !strings.Contains(dsn, "mode=ro") {
		t.Fatalf("bad dsn %q", dsn)
	}
	db, err := OpenPlain(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var qo int
	if err := db.QueryRow("PRAGMA query_only").Scan(&qo); err != nil || qo != 1 {
		t.Fatalf("query_only=%d err=%v", qo, err)
	}
}

func testKeySalt() ([]byte, []byte) {
	return bytes.Repeat([]byte{0x11}, crypto.KeySize), bytes.Repeat([]byte{0x22}, crypto.SaltSize)
}

func writeEncryptedDB(path string, encKey, salt []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}
	iv := bytes.Repeat([]byte{0x33}, aes.BlockSize)
	page := encryptPage(encKey, salt, iv, bytes.Repeat([]byte{'P'}, crypto.PageSize-crypto.ReserveSize-crypto.SaltSize), 1)
	if err := os.WriteFile(path, page, 0o644); err != nil {
		panic(err)
	}
}

func macSalt(salt []byte) []byte {
	out := make([]byte, len(salt))
	for i, b := range salt {
		out[i] = b ^ crypto.HMACXOR
	}
	return out
}

func pageHMAC(encKey, salt, hmacData []byte, pgno uint32) []byte {
	macKey, err := pbkdf2.Key(sha512.New, string(encKey), macSalt(salt), crypto.PBKDF2Iter, crypto.KeySize)
	if err != nil {
		panic(err)
	}
	m := hmac.New(sha512.New, macKey)
	m.Write(hmacData)
	var le [4]byte
	binary.LittleEndian.PutUint32(le[:], pgno)
	m.Write(le[:])
	return m.Sum(nil)
}

func encryptPage(encKey, salt, iv, plain []byte, pgno uint32) []byte {
	block, err := aes.NewCipher(encKey)
	if err != nil {
		panic(err)
	}
	mode := cipher.NewCBCEncrypter(block, iv)
	page := make([]byte, crypto.PageSize)
	copy(page[crypto.PageSize-crypto.ReserveSize:crypto.PageSize-crypto.ReserveSize+16], iv)
	copy(page[:crypto.SaltSize], salt)
	mode.CryptBlocks(page[crypto.SaltSize:crypto.PageSize-crypto.ReserveSize], plain)
	mac := pageHMAC(encKey, salt, page[crypto.SaltSize:crypto.PageSize-crypto.ReserveSize+16], 1)
	copy(page[crypto.PageSize-64:], mac)
	return page
}
