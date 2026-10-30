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
	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowLongW           = user32.NewProc("GetWindowLongW")
	procSetWindowLongW           = user32.NewProc("SetWindowLongW")
	procGetCurrentProcessId      = kernel32.NewProc("GetCurrentProcessId")
	procGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")
)

// GetAppWindowHWND discovers our own application top-level window by Process ID
func GetAppWindowHWND() uintptr {
	var appHwnd uintptr
	curPid, _, _ := procGetCurrentProcessId.Call()

	cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		var pid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if uintptr(pid) == curPid {
			vis, _, _ := procIsWindowVisible.Call(hwnd)
			if vis != 0 {
				appHwnd = hwnd
				return 0 // found target window, stop enumeration
			}
		}
		return 1 // continue searching
	})

	procEnumWindows.Call(cb, 0)
	return appHwnd
}

// ForceFullscreenLockbox locks the entire monitor screen covering 100% of the display and topmost over everything
func ForceFullscreenLockbox() {
	hwnd := GetAppWindowHWND()
	if hwnd == 0 {
		// Fallback to title lookup if EnumWindows hasn't settled yet
		ptr, _ := syscall.UTF16PtrFromString("SnoopTrap // IntruderLens")
		hwnd, _, _ = procFindWindowW.Call(0, uintptr(unsafe.Pointer(ptr)))
		if hwnd == 0 {
			return
		}
	}

	cx, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
	cy, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
	if cx == 0 {
		cx = 1920
	}
	if cy == 0 {
		cy = 1080
	}

	// 1. Remove title bar and resizing borders (WS_CAPTION | WS_THICKFRAME) so it's a seamless edge-to-edge lock screen
	// GWL_STYLE = -16
	oldStyle, _, _ := procGetWindowLongW.Call(hwnd, ^uintptr(15)) // -16
	newStyle := oldStyle &^ (0x00C00000 | 0x00040000)
	procSetWindowLongW.Call(hwnd, ^uintptr(15), newStyle)

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

	// 3. HWND_TOPMOST (-1) covering 100% of the primary display at (0, 0, cx, cy)
	// SWP_SHOWWINDOW (0x0040) | SWP_FRAMECHANGED (0x0020)
	procSetWindowPos.Call(hwnd, ^uintptr(0), 0, 0, cx, cy, 0x0040|0x0020)
	procShowWindow.Call(hwnd, 3) // SW_SHOWMAXIMIZED
	procBringWindowToTop.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)
	procSwitchToThisWindow.Call(hwnd, 1)
}

// ForceBringToFront maintains compatibility with caller
func ForceBringToFront(windowTitle string) {
	ForceFullscreenLockbox()
}

// RestoreNormalWindow restores standard window borders and position when disarmed
func RestoreNormalWindow() {
	hwnd := GetAppWindowHWND()
	if hwnd == 0 {
		return
	}
	// Restore standard overlapped window style: WS_OVERLAPPEDWINDOW (0x00CF0000)
	oldStyle, _, _ := procGetWindowLongW.Call(hwnd, ^uintptr(15))
	newStyle := oldStyle | 0x00CF0000
	procSetWindowLongW.Call(hwnd, ^uintptr(15), newStyle)
	procShowWindow.Call(hwnd, 9) // SW_RESTORE
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

