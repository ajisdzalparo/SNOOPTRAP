package failsafe

import (
	"os/exec"
	"strings"
	"syscall"
)

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procLockWorkStation   = user32.NewProc("LockWorkStation")
)

// LockWindows executes Windows native LockScreen immediately
func LockWindows() error {
	r1, _, err := procLockWorkStation.Call()
	if r1 == 0 {
		// Fallback to rundll32
		return exec.Command("rundll32.exe", "user32.dll,LockWorkStation").Run()
	}
	_ = err
	return nil
}

// KillSensitiveProcess terminates specific sensitive process (e.g. WhatsApp.exe)
func KillSensitiveProcess(processName string) error {
	if strings.TrimSpace(processName) == "" {
		processName = "WhatsApp.exe"
	}
	return exec.Command("taskkill", "/IM", processName, "/F").Run()
}

// ExecuteNuclearOption triggers the configured fail-safe action
func ExecuteNuclearOption(action string, targetProcess string) error {
	if action == "taskkill" {
		return KillSensitiveProcess(targetProcess)
	}
	// Default: lock Windows workstation
	return LockWindows()
}

// GetForegroundWindowHandle gets the HWND of the currently active window
func GetForegroundWindowHandle() uintptr {
	hwnd, _, _ := procGetForegroundWindow.Call()
	return hwnd
}

