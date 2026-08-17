//go:build windows

package backend

import (
	"encoding/base64"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// protectPassword 使用当前用户的 DPAPI 加密实例密码。
func protectPassword(instance, password string) (string, error) {
	blob, err := cryptProtect([]byte(password), dpapiEntropy(instance))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(blob), nil
}

// unprotectPassword 解密由当前用户 DPAPI 保护的实例密码。
func unprotectPassword(instance, encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode encrypted password: %w", err)
	}
	plain, err := cryptUnprotect(raw, dpapiEntropy(instance))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// dpapiEntropy 为每个实例提供独立的可选熵，避免密文跨实例复用。
func dpapiEntropy(instance string) []byte {
	return []byte("wxctl:" + instance)
}

// cryptProtect 调用 CryptProtectData。
func cryptProtect(data, entropy []byte) ([]byte, error) {
	in := newDataBlob(data)
	ent := newDataBlob(entropy)
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, entropyBlob(ent), 0, nil, 0, &out); err != nil {
		return nil, fmt.Errorf("CryptProtectData: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobBytes(out), nil
}

// cryptUnprotect 调用 CryptUnprotectData。
func cryptUnprotect(data, entropy []byte) ([]byte, error) {
	in := newDataBlob(data)
	ent := newDataBlob(entropy)
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, entropyBlob(ent), 0, nil, 0, &out); err != nil {
		return nil, fmt.Errorf("CryptUnprotectData: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return blobBytes(out), nil
}

func newDataBlob(b []byte) windows.DataBlob {
	if len(b) == 0 {
		return windows.DataBlob{}
	}
	return windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func entropyBlob(b windows.DataBlob) *windows.DataBlob {
	if b.Size == 0 || b.Data == nil {
		return nil
	}
	return &b
}

func blobBytes(b windows.DataBlob) []byte {
	if b.Size == 0 || b.Data == nil {
		return nil
	}
	out := make([]byte, b.Size)
	copy(out, unsafe.Slice(b.Data, b.Size))
	return out
}
