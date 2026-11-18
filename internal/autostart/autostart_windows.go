//go:build windows

package autostart

import (
	"log"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	appName    = "ZKTecoRESTfulAPI"
)

// EnsureRegistered ensures the executable is registered in the current user's Windows Startup registry.
// This requires NO administrator privileges and runs automatically on user login.
func EnsureRegistered() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return err
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	targetVal := `"` + exePath + `" --silent`
	val, _, err := key.GetStringValue(appName)
	if err == nil && val == targetVal {
		return nil // Already correctly registered
	}

	if err := key.SetStringValue(appName, targetVal); err != nil {
		return err
	}

	log.Printf("[AutoStart] Registered to start automatically on Windows boot: %s", targetVal)
	return nil
}

// Unregister removes the application from the Windows Startup registry.
func Unregister() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	return key.DeleteValue(appName)
}

// HideConsoleWindow hides the command prompt window if running in silent/background mode.
func HideConsoleWindow() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	user32 := syscall.NewLazyDLL("user32.dll")
	getConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	showWindow := user32.NewProc("ShowWindow")

	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd != 0 {
		showWindow.Call(hwnd, 0) // SW_HIDE = 0
	}
}
