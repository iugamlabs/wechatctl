package backend

import (
	"crypto/rand"
	"fmt"
	"io"
)

const (
	passwordUpper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	passwordLower   = "abcdefghijkmnopqrstuvwxyz"
	passwordDigits  = "23456789"
	passwordSpecial = "!@#$%^&*-_+"
	passwordAll     = passwordUpper + passwordLower + passwordDigits + passwordSpecial
)

// generatePassword 生成满足 Windows 复杂度策略的随机密码。
func generatePassword(n int) (string, error) {
	if n < 12 {
		n = 16
	}
	classes := []string{passwordUpper, passwordLower, passwordDigits, passwordSpecial}
	out := make([]byte, n)
	for i, class := range classes {
		b, err := randByte(len(class))
		if err != nil {
			return "", err
		}
		out[i] = class[b]
	}
	for i := len(classes); i < n; i++ {
		b, err := randByte(len(passwordAll))
		if err != nil {
			return "", err
		}
		out[i] = passwordAll[b]
	}
	if err := shuffleBytes(out); err != nil {
		return "", err
	}
	return string(out), nil
}

// randByte 返回 [0, max) 范围内的均匀随机数。
func randByte(max int) (int, error) {
	if max <= 0 {
		return 0, fmt.Errorf("invalid rand range")
	}
	var buf [1]byte
	// 拒绝采样，避免模偏差。
	limit := 256 - (256 % max)
	for {
		if _, err := io.ReadFull(rand.Reader, buf[:]); err != nil {
			return 0, err
		}
		v := int(buf[0])
		if v < limit {
			return v % max, nil
		}
	}
}

// shuffleBytes 使用 Fisher-Yates 打乱密码字符。
func shuffleBytes(b []byte) error {
	for i := len(b) - 1; i > 0; i-- {
		j, err := randByte(i + 1)
		if err != nil {
			return err
		}
		b[i], b[j] = b[j], b[i]
	}
	return nil
}
