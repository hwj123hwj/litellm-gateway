//go:build darwin && !cgo

// Package nativefolder: CGO 禁用的构建（如 mygo build 的沙箱/交叉环境）
// 无法调用 AppKit，回退为不可用，桌面端继续用应用内的网关主机目录浏览器。
package nativefolder

// Pick is unavailable without cgo and always reports cancelled.
func Pick() (string, bool) {
	return "", false
}
