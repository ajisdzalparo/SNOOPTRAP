package failsafe

import (
	"os/exec"
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procLockWorkStation          = user32.NewProc("LockWorkStation")
	procFindWindowW              = user32.NewProc("FindWindowW")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procBringWindowToTop         = user32.NewProc("BringWindowToTop")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSwitchToThisWindow       = user32.NewProc("SwitchToThisWindow")
	procAttachThreadInput        = user32.NewProc("AttachThreadInput")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
)

// ForceBringToFront forces the specified window to the topmost foreground, overriding Windows focus locks
func ForceBringToFront(windowTitle string) {
	ptr, err := syscall.UTF16PtrFromString(windowTitle)
	if err != nil {
		return
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(ptr)))
	if hwnd == 0 {
		return
	}

	// 1. Maximize window
	procShowWindow.Call(hwnd, 3) // SW_SHOWMAXIMIZED

	// 2. Attach thread input to current foreground window to bypass Windows focus lock
	fgHwnd, _, _ := procGetForegroundWindow.Call()
	if fgHwnd != 0 && fgHwnd != hwnd {
		fgThread, _, _ := procGetWindowThreadProcessId.Call(fgHwnd, 0)
		curThread, _, _ := procGetCurrentThreadId.Call()
		if fgThread != 0 && curThread != 0 && fgThread != curThread {
			procAttachThreadInput.Call(curThread, fgThread, 1) // Attach
			procSetForegroundWindow.Call(hwnd)
			procBringWindowToTop.Call(hwnd)
			procAttachThreadInput.Call(curThread, fgThread, 0) // Detach
		}
	}

	// 3. HWND_TOPMOST (-1) covering entire primary screen resolution
	cx, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
	cy, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
	if cx == 0 {
		cx = 1920
	}
	if cy == 0 {
		cy = 1080
	}
	procSetWindowPos.Call(hwnd, ^uintptr(0), 0, 0, cx, cy, 0x0040) // SWP_SHOWWINDOW
	procBringWindowToTop.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)
	procSwitchToThisWindow.Call(hwnd, 1)
}

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

