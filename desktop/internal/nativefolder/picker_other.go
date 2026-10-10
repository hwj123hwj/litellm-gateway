//go:build !darwin

// Package nativefolder: other platforms have no picker binding yet; the
// desktop falls back to the in-app gateway-host browser.
package nativefolder

// Pick is unavailable off macOS and always reports cancelled.
func Pick() (string, bool) {
	return "", false
}
