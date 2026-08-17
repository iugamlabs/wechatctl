package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	// UserPrefix 是 Windows 本地用户的默认前缀，对应 wechatctl_<name>。
	UserPrefix = "wechatctl_"
	// shortUserPrefix 在完整用户名超过 SAM 20 字符限制时使用。
	shortUserPrefix = "wx_"
	maxSAMName      = 20
)

// UsernameForInstance 根据实例名生成合法的 Windows 本地用户名。
// 优先使用 wechatctl_<name>；超过 20 字符时缩短并附加短哈希。
func UsernameForInstance(name string) string {
	u := UserPrefix + name
	if len(u) <= maxSAMName {
		return u
	}
	sum := sha256.Sum256([]byte(strings.ToLower(name)))
	suffix := hex.EncodeToString(sum[:2])
	bodyBudget := maxSAMName - len(shortUserPrefix) - len(suffix)
	body := name
	if len(body) > bodyBudget {
		body = body[:bodyBudget]
	}
	return shortUserPrefix + body + suffix
}

// IsManagedUsername 判断用户名是否由 wxctl 生成，避免误删无关账户。
func IsManagedUsername(username string) bool {
	u := strings.ToLower(username)
	return strings.HasPrefix(u, strings.ToLower(UserPrefix)) || strings.HasPrefix(u, shortUserPrefix)
}
