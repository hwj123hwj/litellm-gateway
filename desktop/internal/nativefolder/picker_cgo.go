//go:build cgo && darwin

// Package nativefolder 把 macOS 原生目录选择面板以 sheet 形式挂到
// EasyGateway 主窗口上，供本机网关连接选择路径。mygo build 默认
// CGO_ENABLED=0，此时编译 picker_nocgo 的回退实现（按钮也随之隐藏，
// 由应用内浏览器接管）。远程连接始终走应用内浏览器。
package nativefolder

/*
#cgo CFLAGS: -x objective-c
#cgo darwin LDFLAGS: -framework AppKit -framework Foundation
#import <AppKit/AppKit.h>

extern void egPickSheetDone(int id, char *path, int ok);

// egBeginSheet 把打开面板挂为窗口 sheet；完成回调在 AppKit 主线程触发。
// sheet 不依赖嵌套 runModal，与 MyGo 的自绘事件循环兼容。
static void egBeginSheet(long handle, int id) {
	NSWindow *win = (NSWindow *)handle;
	NSOpenPanel *panel = [NSOpenPanel openPanel];
	[panel setCanChooseDirectories:YES];
	[panel setCanChooseFiles:NO];
	[panel setAllowsMultipleSelection:NO];
	[panel setCanCreateDirectories:NO];
	[panel setDirectoryURL:[NSURL fileURLWithPath:NSHomeDirectory()]];
	[panel setMessage:@"选择网关主机上的目录"];
	[panel beginSheetModalForWindow:win completionHandler:^(NSModalResponse response) {
		if (response == NSModalResponseOK) {
			char *path = strdup([[[panel URLs] firstObject] path].UTF8String);
			egPickSheetDone(id, path, 1);
			free(path);
		} else {
			egPickSheetDone(id, NULL, 0);
		}
	}];
}
*/
import "C"
import "sync"

var (
	cbMu       sync.Mutex
	cbNextID   = 1
	cbRegistry = map[int]func(string, bool){}
)

// PickSheet 把目录选择面板以 sheet 形式挂到 handle 指向的原生窗口，
// 立即返回；用户确认或关闭面板后 done 被调用恰好一次（AppKit 主线程，
// ok=false 表示取消）。必须在 AppKit 主线程调用。
func PickSheet(handle uintptr, done func(path string, ok bool)) {
	if done == nil || handle == 0 {
		return
	}
	cbMu.Lock()
	id := cbNextID
	cbNextID++
	cbRegistry[id] = done
	cbMu.Unlock()
	C.egBeginSheet(C.long(handle), C.int(id))
}

// egPickSheetDone 是 ObjC 完成回调进入 Go 的蹦床；按 id 找回闭包。
//
//export egPickSheetDone
func egPickSheetDone(id C.int, path *C.char, ok C.int) {
	cbMu.Lock()
	done := cbRegistry[int(id)]
	delete(cbRegistry, int(id))
	cbMu.Unlock()
	if done == nil {
		return
	}
	var picked string
	if path != nil && ok != 0 {
		picked = C.GoString(path)
	}
	done(picked, ok != 0 && picked != "")
}
