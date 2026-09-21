package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func testKey() []byte  { return bytes.Repeat([]byte{0x11}, KeySize) }
func testSalt() []byte { return bytes.Repeat([]byte{0x22}, SaltSize) }
func testIV() []byte   { return bytes.Repeat([]byte{0x33}, aes.BlockSize) }

func macSalt(salt []byte) []byte {
	out := make([]byte, len(salt))
	for i, b := range salt {
		out[i] = b ^ HMACXOR
	}
	return out
}

func pageHMAC(encKey, salt, hmacData []byte, pgno uint32) []byte {
	macKey, err := pbkdf2.Key(sha512.New, string(encKey), macSalt(salt), PBKDF2Iter, KeySize)
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

// encryptPage 仅测试用：构造 SQLCipher-4 页（page1 的 plain 不含 SQLITE_HDR）。
func encryptPage(encKey, salt, iv, plain []byte, pgno uint32) []byte {
	block, err := aes.NewCipher(encKey)
	if err != nil {
		panic(err)
	}
	mode := cipher.NewCBCEncrypter(block, iv)
	page := make([]byte, PageSize)
	copy(page[PageSize-ReserveSize:PageSize-ReserveSize+16], iv)
	if pgno == 1 {
		if len(plain) != PageSize-ReserveSize-SaltSize {
			panic("page1 plaintext must be 4000 bytes")
		}
		copy(page[:SaltSize], salt)
		mode.CryptBlocks(page[SaltSize:PageSize-ReserveSize], plain)
		mac := pageHMAC(encKey, salt, page[SaltSize:PageSize-ReserveSize+16], 1)
		copy(page[PageSize-64:], mac)
		return page
	}
	if len(plain) != PageSize-ReserveSize {
		panic("page n plaintext must be 4016 bytes")
	}
	mode.CryptBlocks(page[:PageSize-ReserveSize], plain)
	mac := pageHMAC(encKey, salt, page[:PageSize-ReserveSize+16], pgno)
	copy(page[PageSize-64:], mac)
	return page
}

func TestDecryptPage1(t *testing.T) {
	encKey, salt, iv := testKey(), testSalt(), testIV()
	body := bytes.Repeat([]byte{'P'}, PageSize-ReserveSize-SaltSize)
	page := encryptPage(encKey, salt, iv, body, 1)

	got, err := DecryptPage(encKey, page, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[:len(SQLiteHdr)], SQLiteHdr) {
		t.Fatalf("missing sqlite header: %q", got[:16])
	}
	if !bytes.Equal(got[len(SQLiteHdr):PageSize-ReserveSize], body) {
		t.Fatal("page1 plaintext mismatch")
	}
	if !bytes.Equal(got[PageSize-ReserveSize:], make([]byte, ReserveSize)) {
		t.Fatal("page1 reserve should be 80 zeros")
	}
}

func TestEncryptPagesMatchWechatCLI(t *testing.T) {
	encKey, salt, iv := testKey(), testSalt(), testIV()
	p1 := encryptPage(encKey, salt, iv, bytes.Repeat([]byte{'P'}, PageSize-ReserveSize-SaltSize), 1)
	p2 := encryptPage(encKey, salt, iv, bytes.Repeat([]byte{'Q'}, PageSize-ReserveSize), 2)
	sum1 := sha256.Sum256(p1)
	sum2 := sha256.Sum256(p2)
	// 由 wechat-cli Crypto.Cipher + verify_enc_key 对同一 key/salt/iv/plain 算出。
	const want1 = "9b695bbebbc876a29d2b6c03919e3ee4ceecad2f9931dd44edc8e656af4bd883"
	const want2 = "dfbbf8236c381c83393bade4ab4c31d09b37867bde4ee5dc29834d4985793290"
	if hex.EncodeToString(sum1[:]) != want1 {
		t.Fatalf("page1 sha256=%s, want %s", hex.EncodeToString(sum1[:]), want1)
	}
	if hex.EncodeToString(sum2[:]) != want2 {
		t.Fatalf("page2 sha256=%s, want %s", hex.EncodeToString(sum2[:]), want2)
	}
}

func TestDecryptPage2RoundTrip(t *testing.T) {
	encKey, salt, iv := testKey(), testSalt(), testIV()
	body := bytes.Repeat([]byte{'Q'}, PageSize-ReserveSize)
	page := encryptPage(encKey, salt, iv, body, 2)

	got, err := DecryptPage(encKey, page, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[:PageSize-ReserveSize], body) {
		t.Fatal("page2 plaintext mismatch")
	}
	if !bytes.Equal(got[PageSize-ReserveSize:], make([]byte, ReserveSize)) {
		t.Fatal("page2 reserve should be 80 zeros")
	}
}

func TestFullDecryptIgnoresTrailingPartialPage(t *testing.T) {
	dir := t.TempDir()
	encKey, salt, iv := testKey(), testSalt(), testIV()
	body := bytes.Repeat([]byte{'R'}, PageSize-ReserveSize-SaltSize)
	page := encryptPage(encKey, salt, iv, body, 1)

	encPath := filepath.Join(dir, "enc.db")
	raw := append(page, bytes.Repeat([]byte{0xFF}, 100)...)
	if len(raw) != PageSize+100 {
		t.Fatalf("fixture size=%d", len(raw))
	}
	if err := os.WriteFile(encPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(dir, "out", "nested")
	outPath := filepath.Join(outDir, "plain.db")
	n, err := FullDecrypt(encPath, outPath, encKey)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pages=%d, want 1", n)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != PageSize {
		t.Fatalf("out size=%d, want %d (must not decrypt the 100-byte tail)", len(got), PageSize)
	}
	if !bytes.Equal(got[:len(SQLiteHdr)], SQLiteHdr) {
		t.Fatal("decrypted page missing sqlite header")
	}
	st, err := os.Stat(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("parent dir perm=%o, want 0700", st.Mode().Perm())
	}
}

func TestDecryptWALWritesPage(t *testing.T) {
	dir := t.TempDir()
	encKey, salt, iv := testKey(), testSalt(), testIV()
	body := bytes.Repeat([]byte{'W'}, PageSize-ReserveSize)
	encPage := encryptPage(encKey, salt, iv, body, 2)
	decPage, err := DecryptPage(encKey, encPage, 2)
	if err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "plain.db")
	initial := bytes.Repeat([]byte{'X'}, PageSize*2)
	if err := os.WriteFile(outPath, initial, 0o600); err != nil {
		t.Fatal(err)
	}

	const salt1 uint32 = 0xaabbccdd
	const salt2 uint32 = 0x11223344
	wal := makeWAL(salt1, salt2, []walFrame{
		{pgno: 0, salt1: salt1, salt2: salt2, page: encPage},
		{pgno: 2, salt1: salt1 + 1, salt2: salt2, page: encPage},
		{pgno: 2, salt1: salt1, salt2: salt2, page: encPage},
		{pgno: maxWALPageNo + 1, salt1: salt1, salt2: salt2, page: encPage},
	})
	walPath := filepath.Join(dir, "plain.db-wal")
	if err := os.WriteFile(walPath, wal, 0o600); err != nil {
		t.Fatal(err)
	}

	patched, err := DecryptWAL(walPath, outPath, encKey)
	if err != nil {
		t.Fatal(err)
	}
	if patched != 1 {
		t.Fatalf("patched=%d, want 1 (pgno 0 / salt mismatch / huge pgno skipped)", patched)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[:PageSize], initial[:PageSize]) {
		t.Fatal("page 1 should be unchanged")
	}
	if !bytes.Equal(got[PageSize:PageSize*2], decPage) {
		t.Fatal("WAL did not write decrypted page 2")
	}
}

func TestDecryptWALMissingFile(t *testing.T) {
	n, err := DecryptWAL(filepath.Join(t.TempDir(), "no.wal"), filepath.Join(t.TempDir(), "no.db"), testKey())
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("patched=%d, want 0", n)
	}
}

type walFrame struct {
	pgno         uint32
	salt1, salt2 uint32
	page         []byte
}

func makeWAL(hdrSalt1, hdrSalt2 uint32, frames []walFrame) []byte {
	buf := make([]byte, WALHeaderSize)
	binary.BigEndian.PutUint32(buf[16:20], hdrSalt1)
	binary.BigEndian.PutUint32(buf[20:24], hdrSalt2)
	for _, fr := range frames {
		fh := make([]byte, WALFrameHeaderSize)
		binary.BigEndian.PutUint32(fh[0:4], fr.pgno)
		binary.BigEndian.PutUint32(fh[8:12], fr.salt1)
		binary.BigEndian.PutUint32(fh[12:16], fr.salt2)
		buf = append(buf, fh...)
		buf = append(buf, fr.page...)
	}
	return buf
}
