//go:build !windows

package ui

import "fyne.io/fyne/v2"

// getWindowPosition is not supported on this platform.
func getWindowPosition(_ fyne.Window) (x, y int, ok bool) {
	return 0, 0, false
}

// setWindowPosition is not supported on this platform.
func setWindowPosition(_ fyne.Window, _, _ int) {}
