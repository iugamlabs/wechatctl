package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	PageSize           = 4096
	KeySize            = 32
	SaltSize           = 16
	ReserveSize        = 80 // IV 16 + HMAC-SHA512 64
	WALHeaderSize      = 32
	WALFrameHeaderSize = 24
	HMACXOR            = 0x3A
	PBKDF2Iter         = 2
	maxWALPageNo       = 1_000_000
)

// SQLiteHdr 是解密后 page1 前 16 字节（加密页里被 salt 替换）。
var SQLiteHdr = []byte("SQLite format 3\x00")

// DecryptPage 解密一页 SQLCipher-4 密文。
// AES 密钥就是 enc_key，不再用 salt 做 PBKDF2；CBC 不验证 HMAC。
func DecryptPage(encKey, page []byte, pgno uint32) ([]byte, error) {
	if len(page) < PageSize {
		return nil, fmt.Errorf("encrypted page length %d < %d", len(page), PageSize)
	}
	page = page[:PageSize]

	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	iv := page[PageSize-ReserveSize : PageSize-ReserveSize+16]
	mode := cipher.NewCBCDecrypter(block, iv)

	out := make([]byte, PageSize)
	if pgno == 1 {
		encrypted := page[SaltSize : PageSize-ReserveSize]
		plain := out[len(SQLiteHdr) : len(SQLiteHdr)+len(encrypted)]
		mode.CryptBlocks(plain, encrypted)
		copy(out, SQLiteHdr)
		return out, nil
	}
	encrypted := page[:PageSize-ReserveSize]
	mode.CryptBlocks(out[:len(encrypted)], encrypted)
	return out, nil
}

// FullDecrypt 只循环完整 4096 字节页；尾部残渣不当成下一页。
// 单次读短于一页时 0 填充后再解密（防御截断，不是多出来的页）。
func FullDecrypt(encPath, outPath string, encKey []byte) (n int, err error) {
	st, err := os.Stat(encPath)
	if err != nil {
		return 0, err
	}
	totalPages := int(st.Size()) / PageSize

	if dir := filepath.Dir(outPath); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return 0, err
		}
	}

	in, err := os.Open(encPath)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	tmp := outPath + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			out.Close()
			os.Remove(tmp)
		}
	}()

	var page [PageSize]byte
	for pgno := uint32(1); pgno <= uint32(totalPages); pgno++ {
		clear(page[:])
		nr, readErr := in.Read(page[:])
		if nr == 0 {
			if readErr != nil && readErr != io.EOF {
				err = readErr
				return 0, err
			}
			break
		}
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			err = readErr
			return 0, err
		}
		var dec []byte
		dec, err = DecryptPage(encKey, page[:], pgno)
		if err != nil {
			return 0, err
		}
		if _, err = out.Write(dec); err != nil {
			return 0, err
		}
		n++
	}
	if err = out.Close(); err != nil {
		return 0, err
	}
	if err = os.Rename(tmp, outPath); err != nil {
		return 0, err
	}
	return n, nil
}

// DecryptWAL 把 WAL frame 解密写回对应页。header/frame 是 big-endian。
func DecryptWAL(walPath, outPath string, encKey []byte) (int, error) {
	st, err := os.Stat(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	walSize := st.Size()
	if walSize <= WALHeaderSize {
		return 0, nil
	}

	wf, err := os.Open(walPath)
	if err != nil {
		return 0, err
	}
	defer wf.Close()

	df, err := os.OpenFile(outPath, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer df.Close()

	hdr := make([]byte, WALHeaderSize)
	if _, err := io.ReadFull(wf, hdr); err != nil {
		return 0, err
	}
	walSalt1 := binary.BigEndian.Uint32(hdr[16:20])
	walSalt2 := binary.BigEndian.Uint32(hdr[20:24])

	frameSize := int64(WALFrameHeaderSize + PageSize)
	patched := 0
	fh := make([]byte, WALFrameHeaderSize)
	ep := make([]byte, PageSize)
	for {
		pos, err := wf.Seek(0, io.SeekCurrent)
		if err != nil {
			return patched, err
		}
		if pos+frameSize > walSize {
			break
		}
		if _, err := io.ReadFull(wf, fh); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return patched, err
		}
		pgno := binary.BigEndian.Uint32(fh[0:4])
		frameSalt1 := binary.BigEndian.Uint32(fh[8:12])
		frameSalt2 := binary.BigEndian.Uint32(fh[12:16])
		if _, err := io.ReadFull(wf, ep); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return patched, err
		}
		if pgno == 0 || pgno > maxWALPageNo {
			continue
		}
		if frameSalt1 != walSalt1 || frameSalt2 != walSalt2 {
			continue
		}
		dec, err := DecryptPage(encKey, ep, pgno)
		if err != nil {
			return patched, err
		}
		if _, err := df.WriteAt(dec, int64(pgno-1)*PageSize); err != nil {
			return patched, err
		}
		patched++
	}
	return patched, nil
}
