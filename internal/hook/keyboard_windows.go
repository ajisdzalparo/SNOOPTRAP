package hook

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	WH_KEYBOARD_LL = 13
	WM_KEYDOWN     = 0x0100
	WM_KEYUP       = 0x0101
	WM_SYSKEYDOWN  = 0x0104
	WM_SYSKEYUP    = 0x0105

	VK_TAB     = 0x09
	VK_ESCAPE  = 0x1B
	VK_F4      = 0x73
	VK_LWIN    = 0x5B
	VK_RWIN    = 0x5C
	VK_CONTROL = 0x11
	VK_LCONTROL = 0xA2
	VK_RCONTROL = 0xA3
	VK_U       = 0x55

	LLKHF_ALTDOWN = 0x20
)

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type MSG struct {
	Hwnd    uintptr
	Message uint32
	Wparam  uintptr
	Lparam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

var (
	user32Dll               = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookEx    = user32Dll.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32Dll.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32Dll.NewProc("CallNextHookEx")
	procGetKeyState         = user32Dll.NewProc("GetKeyState")
	procGetMessage          = user32Dll.NewProc("GetMessageW")
	procTranslateMessage    = user32Dll.NewProc("TranslateMessage")
	procDispatchMessage     = user32Dll.NewProc("DispatchMessageW")

	hookHandle     uintptr
	hookMutex      sync.Mutex
	isBlocked      bool
	hookStarted    bool
	secretDisarmFn func()
)

// SetSecretDisarmCallback sets the callback executed when Ctrl+Alt+U is pressed
func SetSecretDisarmCallback(fn func()) {
	hookMutex.Lock()
	defer hookMutex.Unlock()
	secretDisarmFn = fn
}

func keyboardProc(nCode int, wParam uintptr, lParam *KBDLLHOOKSTRUCT) uintptr {
	if nCode >= 0 && lParam != nil {
		kbd := lParam
		isAltDown := (kbd.Flags & LLKHF_ALTDOWN) != 0

		// Check Ctrl key state
		ctrlState, _, _ := procGetKeyState.Call(uintptr(VK_CONTROL))
		isCtrlDown := (int16(ctrlState) < 0)

		// 1. Detect Secret Hotkey: Ctrl + Alt + U
		if (kbd.VkCode == VK_U || kbd.VkCode == 'u' || kbd.VkCode == 'U') && isCtrlDown && isAltDown {
			if secretDisarmFn != nil {
				go secretDisarmFn()
			}
			return 1 // Suppress hotkey
		}

		// 2. Block navigation if system is locked (The Trap)
		if isBlocked {
			// Block Windows Keys (Start menu, Win+D, Win+L, etc)
			if kbd.VkCode == VK_LWIN || kbd.VkCode == VK_RWIN {
				return 1
			}
			// Block Alt+Tab
			if kbd.VkCode == VK_TAB && isAltDown {
				return 1
			}
			// Block Alt+F4
			if kbd.VkCode == VK_F4 && isAltDown {
				return 1
			}
			// Block Alt+Esc
			if kbd.VkCode == VK_ESCAPE && isAltDown {
				return 1
			}
			// Block Ctrl+Esc (Start Menu)
			if kbd.VkCode == VK_ESCAPE && isCtrlDown {
				return 1
			}
		}
	}

	r1, _, _ := procCallNextHookEx.Call(hookHandle, uintptr(nCode), wParam, uintptr(unsafe.Pointer(lParam)))
	return r1
}

// InstallHook launches a dedicated OS thread with a Win32 message pump
func InstallHook() {
	hookMutex.Lock()
	defer hookMutex.Unlock()

	if hookStarted {
		return
	}
	hookStarted = true

	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		cb := syscall.NewCallback(keyboardProc)
		h, _, _ := procSetWindowsHookEx.Call(
			uintptr(WH_KEYBOARD_LL),
			cb,
			0,
			0,
		)
		hookHandle = h
		close(ready)

		var msg MSG
		for {
			r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}()

	<-ready
}

// EnableKeyboardBlocker activates the suppression of Windows Key, Alt+Tab, Alt+F4
func EnableKeyboardBlocker() {
	hookMutex.Lock()
	defer hookMutex.Unlock()
	isBlocked = true
}

// DisableKeyboardBlocker restores regular keyboard shortcuts
func DisableKeyboardBlocker() {
	hookMutex.Lock()
	defer hookMutex.Unlock()
	isBlocked = false
}
