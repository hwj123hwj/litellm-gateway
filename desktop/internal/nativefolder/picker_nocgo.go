//go:build !cgo || !darwin

// Package nativefolder：本构建（CGO_ENABLED=0 的 mygo build 或非 macOS）
// 不含原生面板，PickSheet 以取消结束；界面回退到应用内目录浏览器。
package nativefolder

// PickSheet 不可用，立即以取消回调结束。
func PickSheet(handle uintptr, done func(path string, ok bool)) {
	if done != nil {
		go done("", false)
	}
}
