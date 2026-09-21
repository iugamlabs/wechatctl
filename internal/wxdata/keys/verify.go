package keys

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/sha512"
	"encoding/binary"

	"github.com/star-plan/wechatctl/internal/wxdata/crypto"
)

// VerifyEncKey 用 page1 HMAC-SHA512 校验 enc_key（对齐 wechat-cli verify_enc_key）。
// hmac_data 是密文+IV（page1[16:4032]）；页号以 little-endian uint32 追加。
func VerifyEncKey(encKey, page1 []byte) bool {
	if len(page1) < crypto.PageSize {
		return false
	}
	page1 = page1[:crypto.PageSize]

	salt := page1[:crypto.SaltSize]
	macSalt := make([]byte, crypto.SaltSize)
	for i, b := range salt {
		macSalt[i] = b ^ crypto.HMACXOR
	}

	macKey, err := pbkdf2.Key(sha512.New, string(encKey), macSalt, crypto.PBKDF2Iter, crypto.KeySize)
	if err != nil {
		return false
	}

	hmacData := page1[crypto.SaltSize : crypto.PageSize-crypto.ReserveSize+16]
	stored := page1[crypto.PageSize-64 : crypto.PageSize]

	mac := hmac.New(sha512.New, macKey)
	mac.Write(hmacData)
	var pgno [4]byte
	binary.LittleEndian.PutUint32(pgno[:], 1)
	mac.Write(pgno[:])
	return hmac.Equal(mac.Sum(nil), stored)
}
