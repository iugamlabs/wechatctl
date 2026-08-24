//go:build linux

package config

// defaultWechatBin 返回 Linux 默认微信可执行文件路径。
func defaultWechatBin() string {
	return "/usr/bin/wechat"
}

// defaultIMModule 返回 Linux 默认输入法模块。
func defaultIMModule() string {
	return "fcitx"
}
