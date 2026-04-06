package ui

import (
	"syscall"
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procGetWindowRect = user32.NewProc("GetWindowRect")
	procSetWindowPos  = user32.NewProc("SetWindowPos")
)

type rect struct {
	Left, Top, Right, Bottom int32
}

// getWindowPosition returns the current window position using the Win32 API.
func getWindowPosition(w fyne.Window) (x, y int, ok bool) {
	nw, isNative := w.(driver.NativeWindow)
	if !isNative {
		return 0, 0, false
	}
	var r rect
	var found bool
	nw.RunNative(func(ctx any) {
		wctx, isWin := ctx.(driver.WindowsWindowContext)
		if !isWin {
			return
		}
		ret, _, _ := procGetWindowRect.Call(wctx.HWND, uintptr(unsafe.Pointer(&r)))
		if ret != 0 {
			found = true
		}
	})
	if !found {
		return 0, 0, false
	}
	return int(r.Left), int(r.Top), true
}

// setWindowPosition moves the window to the given position using the Win32 API.
func setWindowPosition(w fyne.Window, x, y int) {
	nw, isNative := w.(driver.NativeWindow)
	if !isNative {
		return
	}
	nw.RunNative(func(ctx any) {
		wctx, isWin := ctx.(driver.WindowsWindowContext)
		if !isWin {
			return
		}
		const swpNoSize = 0x0001
		const swpNoZOrder = 0x0004
		procSetWindowPos.Call(wctx.HWND, 0,
			uintptr(x), uintptr(y), 0, 0,
			swpNoSize|swpNoZOrder)
	})
}
