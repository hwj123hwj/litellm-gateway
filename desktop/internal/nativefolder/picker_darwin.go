//go:build darwin

// Package nativefolder wraps the macOS native directory picker so local
// gateway connections get the familiar system open panel. Remote connections
// keep using the in-app browser over the Admin API.
package nativefolder

/*
#cgo CFLAGS: -x objective-c
#cgo darwin LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>
#import <stdlib.h>

static char *pickDirectory(void) {
	NSOpenPanel *panel = [NSOpenPanel openPanel];
	[panel setCanChooseDirectories:YES];
	[panel setCanChooseFiles:NO];
	[panel setAllowsMultipleSelection:NO];
	[panel setCreatesDirectories:NO];
	[panel setDirectoryURL:[NSURL fileURLWithPath:NSHomeDirectory()]];
	[panel setMessage:@"选择网关主机上的目录"];
	if ([panel runModal] == NSModalResponseOK) {
		return strdup([[[panel URLs] firstObject] path].UTF8String);
	}
	return NULL;
}
*/
import "C"
import "unsafe"

// Pick 打开原生目录选择面板并阻塞到用户确认或取消。必须在 UI 主线程
// 调用（面板自带嵌套事件循环，期间窗口保持可交互）；取消返回 ok=false。
func Pick() (string, bool) {
	p := C.pickDirectory()
	if p == nil {
		return "", false
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p), true
}
