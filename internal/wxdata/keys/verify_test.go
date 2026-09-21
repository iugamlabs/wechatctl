package keys

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha512"
	"encoding/binary"
	"testing"

	"github.com/star-plan/wechatctl/internal/wxdata/crypto"
)

func encryptPage1(encKey, salt, iv, body []byte) []byte {
	block, err := aes.NewCipher(encKey)
	if err != nil {
		panic(err)
	}
	page := make([]byte, crypto.PageSize)
	copy(page[:crypto.SaltSize], salt)
	copy(page[crypto.PageSize-crypto.ReserveSize:crypto.PageSize-crypto.ReserveSize+16], iv)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(page[crypto.SaltSize:crypto.PageSize-crypto.ReserveSize], body)
	copy(page[crypto.PageSize-64:], page1MAC(encKey, salt, page[crypto.SaltSize:crypto.PageSize-crypto.ReserveSize+16], binary.LittleEndian))
	return page
}

func page1MAC(encKey, salt, hmacData []byte, order binary.ByteOrder) []byte {
	macSalt := make([]byte, len(salt))
	for i, b := range salt {
		macSalt[i] = b ^ crypto.HMACXOR
	}
	macKey, err := pbkdf2.Key(sha512.New, string(encKey), macSalt, crypto.PBKDF2Iter, crypto.KeySize)
	if err != nil {
		panic(err)
	}
	m := hmac.New(sha512.New, macKey)
	m.Write(hmacData)
	var pgno [4]byte
	order.PutUint32(pgno[:], 1)
	m.Write(pgno[:])
	return m.Sum(nil)
}

func TestVerifyEncKey(t *testing.T) {
	encKey := bytes.Repeat([]byte{0x11}, crypto.KeySize)
	salt := bytes.Repeat([]byte{0x22}, crypto.SaltSize)
	iv := bytes.Repeat([]byte{0x33}, aes.BlockSize)
	body := bytes.Repeat([]byte{'P'}, crypto.PageSize-crypto.ReserveSize-crypto.SaltSize)
	page := encryptPage1(encKey, salt, iv, body)

	if !VerifyEncKey(encKey, page) {
		t.Fatal("VerifyEncKey=false, want true")
	}

	wrong := bytes.Clone(encKey)
	wrong[0] ^= 0xff
	if VerifyEncKey(wrong, page) {
		t.Fatal("wrong key must fail HMAC")
	}

	short := page[:crypto.PageSize-1]
	if VerifyEncKey(encKey, short) {
		t.Fatal("short page1 must fail")
	}
}

func TestVerifyEncKeyRejectsBigEndianPageNumber(t *testing.T) {
	encKey := bytes.Repeat([]byte{0x11}, crypto.KeySize)
	salt := bytes.Repeat([]byte{0x22}, crypto.SaltSize)
	iv := bytes.Repeat([]byte{0x33}, aes.BlockSize)
	body := bytes.Repeat([]byte{'P'}, crypto.PageSize-crypto.ReserveSize-crypto.SaltSize)
	page := encryptPage1(encKey, salt, iv, body)
	copy(page[crypto.PageSize-64:], page1MAC(encKey, salt, page[crypto.SaltSize:crypto.PageSize-crypto.ReserveSize+16], binary.BigEndian))
	if VerifyEncKey(encKey, page) {
		t.Fatal("HMAC with big-endian page number must not verify")
	}
}
