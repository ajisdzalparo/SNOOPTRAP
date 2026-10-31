package failsafe

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

type winRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

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
	procIsWindow                 = user32.NewProc("IsWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procIsIconic                 = user32.NewProc("IsIconic")
	procGetWindowLongW           = user32.NewProc("GetWindowLongW")
	procSetWindowLongW           = user32.NewProc("SetWindowLongW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetParent                = user32.NewProc("GetParent")
	procGetAncestor              = user32.NewProc("GetAncestor")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procGetSystemMenu            = user32.NewProc("GetSystemMenu")
	procEnableMenuItem           = user32.NewProc("EnableMenuItem")
	procClipCursor               = user32.NewProc("ClipCursor")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procGetCurrentProcessId      = kernel32.NewProc("GetCurrentProcessId")
	procGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")

	hwndMu           sync.Mutex
	cachedAppHwnd    uintptr
	enumSearchPid    uintptr
	enumExactMatch   uintptr
	enumFallbackHwnd uintptr
	enumCbOnce       sync.Once
	enumCbPtr        uintptr

	lockboxMu        sync.Mutex
	isLockdownActive atomic.Bool
	watchdogRunning  atomic.Bool
)

func enumWindowsProc(hwnd uintptr, lParam uintptr) uintptr {
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if uintptr(pid) == enumSearchPid {
		var clsBuf [128]uint16
		clsLen, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&clsBuf[0])), 128)
		clsName := ""
		if clsLen > 0 {
			clsName = syscall.UTF16ToString(clsBuf[:clsLen])
		}

		var buf [256]uint16
		textLen, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
		title := ""
		if textLen > 0 {
			title = syscall.UTF16ToString(buf[:textLen])
		}

		// Wails v2 creates the primary window with class "wailsWindow"
		if strings.EqualFold(clsName, "wailsWindow") || strings.Contains(strings.ToLower(clsName), "wails") {
			enumExactMatch = hwnd
			return 0 // Found exact main Wails window, stop enumeration
		}

		if strings.Contains(strings.ToLower(title), "snooptrap") {
			enumExactMatch = hwnd
			return 0
		}

		parent, _, _ := procGetParent.Call(hwnd)
		if parent == 0 && enumFallbackHwnd == 0 {
			enumFallbackHwnd = hwnd
		}
	}
	return 1
}

// GetAppWindowHWND discovers and caches our own application top-level window by Process ID (zero callback leak)
func GetAppWindowHWND() uintptr {
	hwndMu.Lock()
	defer hwndMu.Unlock()

	if cachedAppHwnd != 0 {
		ok, _, _ := procIsWindow.Call(cachedAppHwnd)
		if ok != 0 {
			return cachedAppHwnd
		}
		cachedAppHwnd = 0
	}

	// 1. Direct search by Wails native window class
	wailsClassPtr, _ := syscall.UTF16PtrFromString("wailsWindow")
	hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(wailsClassPtr)), 0)
	if hwnd != 0 {
		var pid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		curPid, _, _ := procGetCurrentProcessId.Call()
		if uintptr(pid) == curPid {
			cachedAppHwnd = hwnd
			return hwnd
		}
	}

	// 2. Direct search by Window Title
	ptr, _ := syscall.UTF16PtrFromString("SnoopTrap // IntruderLens")
	hwnd, _, _ = procFindWindowW.Call(0, uintptr(unsafe.Pointer(ptr)))
	if hwnd != 0 {
		cachedAppHwnd = hwnd
		return hwnd
	}

	// 3. Enumeration across current process threads
	enumCbOnce.Do(func() {
		enumCbPtr = syscall.NewCallback(enumWindowsProc)
	})

	curPid, _, _ := procGetCurrentProcessId.Call()
	enumSearchPid = curPid
	enumExactMatch = 0
	enumFallbackHwnd = 0

	procEnumWindows.Call(enumCbPtr, 0)

	if enumExactMatch != 0 {
		cachedAppHwnd = enumExactMatch
		return cachedAppHwnd
	}
	if enumFallbackHwnd != 0 {
		cachedAppHwnd = enumFallbackHwnd
		return enumFallbackHwnd
	}

	return 0
}

func getTargetScreenBounds() (x, y, w, h, primaryW, primaryH uintptr) {
	cx, _, _ := procGetSystemMetrics.Call(0) // SM_CXSCREEN
	cy, _, _ := procGetSystemMetrics.Call(1) // SM_CYSCREEN
	if cx == 0 {
		cx = 1920
	}
	if cy == 0 {
		cy = 1080
	}
	primaryW = cx
	primaryH = cy

	vx, _, _ := procGetSystemMetrics.Call(76) // SM_XVIRTUALSCREEN
	vy, _, _ := procGetSystemMetrics.Call(77) // SM_YVIRTUALSCREEN
	vw, _, _ := procGetSystemMetrics.Call(78) // SM_CXVIRTUALSCREEN
	vh, _, _ := procGetSystemMetrics.Call(79) // SM_CYVIRTUALSCREEN

	if vw > 0 && vh > 0 {
		return vx, vy, vw, vh, primaryW, primaryH
	}
	return 0, 0, cx, cy, primaryW, primaryH
}

func applyClipCursor(primaryW, primaryH uintptr) {
	if primaryW > 16 && primaryH > 16 {
		clip := winRect{
			Left:   4,
			Top:    4,
			Right:  int32(primaryW) - 4,
			Bottom: int32(primaryH) - 4,
		}
		procClipCursor.Call(uintptr(unsafe.Pointer(&clip)))
	}
}

func applyLockboxStylesAndFocus(hwnd uintptr) {
	if hwnd == 0 {
		return
	}

	x, y, w, h, primaryW, primaryH := getTargetScreenBounds()

	// Un-minimize if Show Desktop / Win+D made window iconic
	iconic, _, _ := procIsIconic.Call(hwnd)
	if iconic != 0 {
		procShowWindow.Call(hwnd, 9) // SW_RESTORE
	}

	// 1. Strip WS_CAPTION (0x00C00000), WS_THICKFRAME (0x00040000), WS_SYSMENU (0x00080000),
	//    WS_MINIMIZEBOX (0x00020000), WS_MAXIMIZEBOX (0x00010000), WS_BORDER (0x00800000), WS_MAXIMIZE (0x01000000)
	//    and apply WS_POPUP (0x80000000) | WS_VISIBLE (0x10000000)
	oldStyle, _, _ := procGetWindowLongW.Call(hwnd, ^uintptr(15)) // GWL_STYLE = -16
	newStyle := (oldStyle &^ (0x00C00000 | 0x00040000 | 0x00080000 | 0x00020000 | 0x00010000 | 0x00800000 | 0x01000000)) | 0x80000000 | 0x10000000
	procSetWindowLongW.Call(hwnd, ^uintptr(15), newStyle)

	// 2. Set WS_EX_TOPMOST (0x00000008) | WS_EX_TOOLWINDOW (0x00000080) and strip WS_EX_APPWINDOW (0x00040000).
	//    WS_EX_TOOLWINDOW hides SnoopTrap from Taskbar AND Windows 11 Task View (no red [X] close card!)
	//    AND pins the window across ALL Windows 10/11 Virtual Desktops automatically!
	oldExStyle, _, _ := procGetWindowLongW.Call(hwnd, ^uintptr(19)) // GWL_EXSTYLE = -20
	newExStyle := (oldExStyle &^ 0x00040000) | 0x00000008 | 0x00000080
	procSetWindowLongW.Call(hwnd, ^uintptr(19), newExStyle)

	// 3. Disable SC_CLOSE (0xF060) in window system menu
	sysMenu, _, _ := procGetSystemMenu.Call(hwnd, 0)
	if sysMenu != 0 {
		procEnableMenuItem.Call(sysMenu, 0xF060, 0x00000001|0x00000002) // MF_GRAYED | MF_DISABLED
	}

	// 4. Attach thread input to current foreground window to bypass Windows focus lock
	fgHwnd, _, _ := procGetForegroundWindow.Call()
	if fgHwnd != 0 && fgHwnd != hwnd {
		fgThread, _, _ := procGetWindowThreadProcessId.Call(fgHwnd, 0)
		curThread, _, _ := procGetCurrentThreadId.Call()
		if fgThread != 0 && curThread != 0 && fgThread != curThread {
			procAttachThreadInput.Call(curThread, fgThread, 1)
			procShowWindow.Call(hwnd, 5) // SW_SHOW
			procSetWindowPos.Call(hwnd, ^uintptr(0), x, y, w, h, 0x0040|0x0020)
			procBringWindowToTop.Call(hwnd)
			procSetForegroundWindow.Call(hwnd)
			procAttachThreadInput.Call(curThread, fgThread, 0)
		}
	}

	// 5. Force HWND_TOPMOST (-1) covering 100% of screen bounds (without SW_SHOWMAXIMIZED so it covers taskbar!)
	procShowWindow.Call(hwnd, 5)                                        // SW_SHOW
	procSetWindowPos.Call(hwnd, ^uintptr(0), x, y, w, h, 0x0040|0x0020) // HWND_TOPMOST, SWP_SHOWWINDOW | SWP_FRAMECHANGED
	procBringWindowToTop.Call(hwnd)
	procSetForegroundWindow.Call(hwnd)
	procSwitchToThisWindow.Call(hwnd, 1)

	// 6. Confine mouse cursor strictly inside the Lockbox screen
	applyClipCursor(primaryW, primaryH)
}

// ForceFullscreenLockbox locks the entire monitor screen covering 100% of the display and topmost over everything
func ForceFullscreenLockbox() {
	lockboxMu.Lock()
	defer lockboxMu.Unlock()
	hwnd := GetAppWindowHWND()
	applyLockboxStylesAndFocus(hwnd)
}

// StartLockdownWatchdog engages continuous 50ms enforcement so Task View (3-finger swipe),
// Show Desktop, Virtual Desktops, or any external window can never escape or close the Lockbox.
func StartLockdownWatchdog() {
	isLockdownActive.Store(true)
	ForceFullscreenLockbox()

	if !watchdogRunning.CompareAndSwap(false, true) {
		return
	}

	go func() {
		defer watchdogRunning.Store(false)

		// Secondary pass at 150ms after Wails async WindowShow/WindowFullscreen settles
		time.Sleep(150 * time.Millisecond)
		if isLockdownActive.Load() {
			ForceFullscreenLockbox()
		}

		curPid, _, _ := procGetCurrentProcessId.Call()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			if !isLockdownActive.Load() {
				procClipCursor.Call(0)
				return
			}

			lockboxMu.Lock()
			if !isLockdownActive.Load() {
				procClipCursor.Call(0)
				lockboxMu.Unlock()
				return
			}

			hwnd := GetAppWindowHWND()
			if hwnd == 0 {
				lockboxMu.Unlock()
				continue
			}

			_, _, _, _, primaryW, primaryH := getTargetScreenBounds()
			applyClipCursor(primaryW, primaryH)

			vis, _, _ := procIsWindowVisible.Call(hwnd)
			iconic, _, _ := procIsIconic.Call(hwnd)
			exStyle, _, _ := procGetWindowLongW.Call(hwnd, ^uintptr(19))
			missingToolStyle := (exStyle & 0x00000080) == 0

			var r winRect
			procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
			sizeShrank := (r.Right-r.Left) < int32(primaryW) || (r.Bottom-r.Top) < int32(primaryH)

			fgHwnd, _, _ := procGetForegroundWindow.Call()
			lostForeground := false
			if fgHwnd == 0 {
				lostForeground = true
			} else if fgHwnd != hwnd {
				rootHwnd, _, _ := procGetAncestor.Call(fgHwnd, 2) // GA_ROOT = 2
				var fgPid uint32
				procGetWindowThreadProcessId.Call(fgHwnd, uintptr(unsafe.Pointer(&fgPid)))
				if rootHwnd != hwnd && uintptr(fgPid) != curPid {
					lostForeground = true
					// If Windows 10/11 Task View overlay opened (e.g. via 3-finger touchpad swipe), dismiss it immediately
					var clsBuf [128]uint16
					clsLen, _, _ := procGetClassNameW.Call(fgHwnd, uintptr(unsafe.Pointer(&clsBuf[0])), 128)
					if clsLen > 0 {
						clsName := syscall.UTF16ToString(clsBuf[:clsLen])
						if clsName == "XamlExplorerHostIslandWindow" || clsName == "MultitaskingViewFrame" {
							procPostMessageW.Call(fgHwnd, 0x0010, 0, 0) // WM_CLOSE
						}
					}
				}
			}

			if vis == 0 || iconic != 0 || missingToolStyle || sizeShrank || lostForeground {
				applyLockboxStylesAndFocus(hwnd)
			}

			lockboxMu.Unlock()
		}
	}()
}

// StopLockdownWatchdog releases the continuous lockdown watchdog and cursor confinement
func StopLockdownWatchdog() {
	isLockdownActive.Store(false)
	procClipCursor.Call(0)
}

// ForceBringToFront maintains compatibility with caller
func ForceBringToFront(windowTitle string) {
	ForceFullscreenLockbox()
}

// RestoreNormalWindow restores standard window borders, taskbar visibility, and position when disarmed
func RestoreNormalWindow() {
	lockboxMu.Lock()
	defer lockboxMu.Unlock()

	isLockdownActive.Store(false)
	procClipCursor.Call(0) // Release cursor confinement immediately

	hwnd := GetAppWindowHWND()
	if hwnd == 0 {
		return
	}

	// Restore extended window style: remove WS_EX_TOOLWINDOW & WS_EX_TOPMOST, restore WS_EX_APPWINDOW
	oldExStyle, _, _ := procGetWindowLongW.Call(hwnd, ^uintptr(19))
	newExStyle := (oldExStyle &^ (0x00000080 | 0x00000008)) | 0x00040000
	procSetWindowLongW.Call(hwnd, ^uintptr(19), newExStyle)

	// Restore standard overlapped window style: remove WS_POPUP, restore WS_OVERLAPPEDWINDOW (0x00CF0000)
	oldStyle, _, _ := procGetWindowLongW.Call(hwnd, ^uintptr(15))
	newStyle := (oldStyle &^ 0x80000000) | 0x00CF0000
	procSetWindowLongW.Call(hwnd, ^uintptr(15), newStyle)

	// Reset system menu back to default (re-enables Close button in normal mode)
	procGetSystemMenu.Call(hwnd, 1)

	// Remove HWND_TOPMOST (-2 = HWND_NOTOPMOST) and apply frame change
	procSetWindowPos.Call(hwnd, ^uintptr(1), 0, 0, 1024, 768, 0x0002|0x0020) // SWP_NOMOVE | SWP_FRAMECHANGED
	procShowWindow.Call(hwnd, 9)                                              // SW_RESTORE
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

// KillSensitiveProcess terminates specific sensitive processes (e.g. WhatsApp, browsers, messengers)
func KillSensitiveProcess(processName string) error {
	pName := strings.TrimSpace(processName)
	if pName == "" || strings.EqualFold(pName, "WhatsApp.exe") || strings.EqualFold(pName, "WhatsApp") {
		// Kill all variants of WhatsApp (UWP Store app: WhatsApp.Root.exe, Win32: WhatsApp.exe, and all child processes)
		_ = exec.Command("taskkill", "/F", "/T", "/IM", "WhatsApp.Root.exe").Run()
		_ = exec.Command("taskkill", "/F", "/T", "/IM", "WhatsApp.exe").Run()
		_ = exec.Command("taskkill", "/F", "/T", "/IM", "WhatsApp*").Run()
		_ = exec.Command("powershell", "-NoProfile", "-Command", "Get-Process *whatsapp* -ErrorAction SilentlyContinue | Stop-Process -Force").Run()
		return nil
	}

	// For custom process configured by user
	cleanName := strings.TrimSuffix(pName, ".exe")
	_ = exec.Command("taskkill", "/F", "/T", "/IM", pName).Run()
	_ = exec.Command("taskkill", "/F", "/T", "/IM", cleanName+".exe").Run()
	_ = exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf("Get-Process '%s*' -ErrorAction SilentlyContinue | Stop-Process -Force", cleanName)).Run()
	return nil
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

// EnsureWindowsStartup registers SnoopTrap in HKCU\...\Run with zero startup delay so forced reboots immediately restore previous status
func EnsureWindowsStartup() error {
	exePath, err := os.Executable()
	if err != nil || exePath == "" {
		return err
	}
	exePath, _ = filepath.Abs(exePath)

	// Skip temporary go-build / wails dev binaries; prefer installed binary if available
	lower := strings.ToLower(exePath)
	if strings.Contains(lower, "go-build") || strings.Contains(lower, "-dev") {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			installed := filepath.Join(localAppData, "Programs", "SnoopTrap", "snooptrap.exe")
			if _, statErr := os.Stat(installed); statErr == nil {
				exePath = installed
			} else {
				return nil
			}
		} else {
			return nil
		}
	}

	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	cmdValue := fmt.Sprintf(`"%s" --autostart`, exePath)
	err = k.SetStringValue("SnoopTrap", cmdValue)
	_ = k.Close()

	// Disable Windows 10/11 10-second Explorer startup delay so Lockbox appears immediately after Windows login
	serKey, _, serErr := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\Serialize`, registry.SET_VALUE)
	if serErr == nil {
		_ = serKey.SetDWordValue("StartupDelayInMSec", 0)
		_ = serKey.Close()
	}

	return err
}
