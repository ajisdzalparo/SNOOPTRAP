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

	VK_TAB      = 0x09
	VK_ESCAPE   = 0x1B
	VK_F4       = 0x73
	VK_LWIN     = 0x5B
	VK_RWIN     = 0x5C
	VK_CONTROL  = 0x11
	VK_LCONTROL = 0xA2
	VK_RCONTROL = 0xA3
	VK_MENU     = 0x12 // Alt
	VK_LMENU    = 0xA4
	VK_RMENU    = 0xA5
	VK_U        = 0x55

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
	procGetAsyncKeyState   = user32Dll.NewProc("GetAsyncKeyState")
	procGetMessage          = user32Dll.NewProc("GetMessageW")
	procTranslateMessage    = user32Dll.NewProc("TranslateMessage")
	procDispatchMessage     = user32Dll.NewProc("DispatchMessageW")

	hookHandle     uintptr
	hookMutex      sync.Mutex
	isBlocked      bool
	hookStarted    bool
	secretDisarmFn func()
	onKeyPressFn   func()

	// Hardware key tracking
	isCtrlDown  bool
	isAltDown   bool
	isWinDown   bool
	isShiftDown bool
)

// SetSecretDisarmCallback sets the callback executed when Ctrl+Alt+U is pressed
func SetSecretDisarmCallback(fn func()) {
	hookMutex.Lock()
	defer hookMutex.Unlock()
	secretDisarmFn = fn
}

// SetOnKeyPressCallback sets the callback invoked whenever any physical key is pressed
func SetOnKeyPressCallback(fn func()) {
	hookMutex.Lock()
	defer hookMutex.Unlock()
	onKeyPressFn = fn
}

func keyboardProc(nCode int32, wParam uintptr, lParam uintptr) uintptr {
	if nCode < 0 || lParam == 0 {
		r1, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return r1
	}

	kbd := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
	vk := kbd.VkCode
	isKeyDown := (wParam == WM_KEYDOWN || wParam == WM_SYSKEYDOWN)
	isKeyUp := (wParam == WM_KEYUP || wParam == WM_SYSKEYUP)

	// Notify keypress event listener for intentional activity detection
	if isKeyDown {
		hookMutex.Lock()
		kpFn := onKeyPressFn
		hookMutex.Unlock()
		if kpFn != nil {
			go kpFn()
		}
	}

	// Track Ctrl and Alt states continuously
	if vk == VK_CONTROL || vk == VK_LCONTROL || vk == VK_RCONTROL {
		if isKeyDown {
			isCtrlDown = true
		} else if isKeyUp {
			isCtrlDown = false
		}
	}

	if vk == VK_MENU || vk == VK_LMENU || vk == VK_RMENU {
		if isKeyDown {
			isAltDown = true
		} else if isKeyUp {
			isAltDown = false
		}
	}

	if vk == VK_LWIN || vk == VK_RWIN {
		if isKeyDown {
			isWinDown = true
		} else if isKeyUp {
			isWinDown = false
		}
	}

	if vk == 0x10 || vk == 0xA0 || vk == 0xA1 { // VK_SHIFT, VK_LSHIFT, VK_RSHIFT
		if isKeyDown {
			isShiftDown = true
		} else if isKeyUp {
			isShiftDown = false
		}
	}

	hasAltFlag := (kbd.Flags & LLKHF_ALTDOWN) != 0

	// 1. Detect Secret Hotkey: Ctrl + Alt + U
	if vk == VK_U || vk == 'u' || vk == 'U' {
		// Verify via tracked state or global async hardware query
		ctrlActive := isCtrlDown
		if !ctrlActive {
			s, _, _ := procGetAsyncKeyState.Call(uintptr(VK_CONTROL))
			if int16(s) < 0 {
				ctrlActive = true
			}
		}

		altActive := isAltDown || hasAltFlag
		if !altActive {
			s, _, _ := procGetAsyncKeyState.Call(uintptr(VK_MENU))
			if int16(s) < 0 {
				altActive = true
			}
		}

		if ctrlActive && altActive {
			if isKeyDown {
				hookMutex.Lock()
				cb := secretDisarmFn
				hookMutex.Unlock()
				if cb != nil {
					go cb()
				}
			}
			return 1 // Suppress hotkey so it doesn't print 'u'
		}
	}

	// 2. Strict Whitelist Lockdown if system is TRIGGERED (The Trap)
	if isBlocked {
		// Always let modifier KEYUP events pass so Windows never gets a stuck modifier state upon disarm
		isModifierKey := vk == VK_CONTROL || vk == VK_LCONTROL || vk == VK_RCONTROL ||
			vk == VK_MENU || vk == VK_LMENU || vk == VK_RMENU ||
			vk == 0x10 || vk == 0xA0 || vk == 0xA1
		if isKeyUp && isModifierKey {
			r1, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
			return r1
		}

		// Suppress Windows keys completely (both down and up)
		if vk == VK_LWIN || vk == VK_RWIN {
			return 1
		}

		anyModifier := isCtrlDown || isAltDown || hasAltFlag || isWinDown

		// Allow ONLY keys needed to enter the 6-digit OTP on Lockbox (plus Volume Up)
		allowed := false
		if !anyModifier {
			switch {
			case vk >= 0x30 && vk <= 0x39 && !isShiftDown: // Top-row digits 0-9
				allowed = true
			case vk >= 0x60 && vk <= 0x69: // Numpad digits 0-9
				allowed = true
			case vk == 0x90: // VK_NUMLOCK
				allowed = true
			case vk == 0x08 || vk == 0x0D || vk == 0x2E: // Backspace, Enter, Delete
				allowed = true
			case vk == VK_TAB || vk == 0x25 || vk == 0x27: // Unmodified Tab, Left, Right
				allowed = true
			case vk == 0xAF: // VK_VOLUME_UP
				allowed = true
			}
		}

		if !allowed {
			return 1 // Suppress ALL escape shortcuts (Alt+Tab, Win+Tab, Alt+F4, Alt+Space, Ctrl+Shift+Esc, Ctrl+W, F1-F12, Volume Mute/Down, etc.)
		}
	}

	r1, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return r1
}

// EnableKeyboardBlocker engages keyboard restriction
func EnableKeyboardBlocker() {
	hookMutex.Lock()
	defer hookMutex.Unlock()
	isBlocked = true
}

// DisableKeyboardBlocker releases keyboard restriction
func DisableKeyboardBlocker() {
	hookMutex.Lock()
	defer hookMutex.Unlock()
	isBlocked = false
	isWinDown = false
	isShiftDown = false
}

// InstallHook installs the low level keyboard hook
func InstallHook() {
	hookMutex.Lock()
	if hookStarted {
		hookMutex.Unlock()
		return
	}
	hookStarted = true
	hookMutex.Unlock()

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		hookCallback := syscall.NewCallback(keyboardProc)
		hHook, _, _ := procSetWindowsHookEx.Call(
			uintptr(WH_KEYBOARD_LL),
			hookCallback,
			0,
			0,
		)

		hookMutex.Lock()
		hookHandle = hHook
		hookMutex.Unlock()

		var msg MSG
		for {
			r1, _, _ := procGetMessage.Call(
				uintptr(unsafe.Pointer(&msg)),
				0,
				0,
				0,
			)
			if int32(r1) <= 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
		}

		if hookHandle != 0 {
			procUnhookWindowsHookEx.Call(hookHandle)
		}
	}()
}

// UninstallHook removes hook
func UninstallHook() {
	hookMutex.Lock()
	defer hookMutex.Unlock()
	if hookHandle != 0 {
		procUnhookWindowsHookEx.Call(hookHandle)
		hookHandle = 0
	}
	hookStarted = false
}
