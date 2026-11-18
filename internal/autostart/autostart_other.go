//go:build !windows

package autostart

// EnsureRegistered is a no-op on non-Windows platforms.
func EnsureRegistered() error {
	return nil
}

// Unregister is a no-op on non-Windows platforms.
func Unregister() error {
	return nil
}

// HideConsoleWindow is a no-op on non-Windows platforms.
func HideConsoleWindow() {
}
