package hook

import (
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

	VK_TAB    = 0x09
	VK_ESCAPE = 0x1B
	VK_F4     = 0x73
	VK_LWIN   = 0x5B
	VK_RWIN   = 0x5C
	VK_CONTROL = 0x11
	VK_LCONTROL = 0xA2
	VK_RCONTROL = 0xA3

	LLKHF_ALTDOWN = 0x20
)

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

var (
	user32Dll              = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookEx   = user32Dll.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32Dll.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx     = user32Dll.NewProc("CallNextHookEx")
	procGetKeyState        = user32Dll.NewProc("GetKeyState")

	hookHandle uintptr
	hookMutex  sync.Mutex
	isBlocked  bool
)

func keyboardProc(nCode int, wParam uintptr, lParam *KBDLLHOOKSTRUCT) uintptr {
	if nCode >= 0 && isBlocked && lParam != nil {
		kbd := lParam
		isAltDown := (kbd.Flags & LLKHF_ALTDOWN) != 0

		// Check Ctrl key state
		ctrlState, _, _ := procGetKeyState.Call(uintptr(VK_CONTROL))
		isCtrlDown := (int16(ctrlState) < 0)

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
		// Block Windows Keys
		if kbd.VkCode == VK_LWIN || kbd.VkCode == VK_RWIN {
			return 1
		}
	}

	r1, _, _ := procCallNextHookEx.Call(hookHandle, uintptr(nCode), wParam, uintptr(unsafe.Pointer(lParam)))
	return r1
}

// EnableKeyboardBlocker activates the suppression of Alt+Tab, Alt+F4, WinKey
func EnableKeyboardBlocker() {
	hookMutex.Lock()
	defer hookMutex.Unlock()

	isBlocked = true
	if hookHandle == 0 {
		cb := syscall.NewCallback(keyboardProc)
		h, _, _ := procSetWindowsHookEx.Call(
			uintptr(WH_KEYBOARD_LL),
			cb,
			0,
			0,
		)
		hookHandle = h
	}
}

// DisableKeyboardBlocker restores regular keyboard shortcuts
func DisableKeyboardBlocker() {
	hookMutex.Lock()
	defer hookMutex.Unlock()

	isBlocked = false
	if hookHandle != 0 {
		procUnhookWindowsHookEx.Call(hookHandle)
		hookHandle = 0
	}
}
