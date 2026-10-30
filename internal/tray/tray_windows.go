package tray

import (
	"os"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procLoadIconW        = user32.NewProc("LoadIconW")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procSetForeground    = user32.NewProc("SetForegroundWindow")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procExtractIconW     = shell32.NewProc("ExtractIconW")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

const (
	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004

	WM_NULL         = 0x0000
	WM_DESTROY      = 0x0002
	WM_COMMAND      = 0x0111
	WM_USER         = 0x0400
	WM_TRAYICON     = WM_USER + 101
	WM_LBUTTONUP    = 0x0202
	WM_LBUTTONDBLCLK = 0x0203
	WM_RBUTTONUP    = 0x0205

	TPM_RIGHTBUTTON = 0x0002
	TPM_BOTTOMALIGN = 0x0020

	MF_STRING    = 0x0000
	MF_SEPARATOR = 0x0800

	CMD_SHOW   = 1001
	CMD_ARM    = 1002
	CMD_DISARM = 1003
	CMD_QUIT   = 1004
)

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

type POINT struct {
	X int32
	Y int32
}

type MSG struct {
	HWnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

type NOTIFYICONDATAW struct {
	CbSize            uint32
	HWnd              windows.HWND
	UID               uint32
	UFlags            uint32
	UCallbackMessage  uint32
	HIcon             windows.Handle
	SzTip             [128]uint16
	DwState           uint32
	DwStateMask       uint32
	SzInfo            [256]uint16
	UTimeoutOrVersion uint32
	SzInfoTitle       [64]uint16
	DwInfoFlags       uint32
	GuidItem          windows.GUID
	HBalloonIcon      windows.Handle
}

type TrayCallbacks struct {
	OnShow   func()
	OnArm    func()
	OnDisarm func()
	OnQuit   func()
}

var (
	callbacks   TrayCallbacks
	trayHWnd    windows.HWND
	trayNID     NOTIFYICONDATAW
	trayMu      sync.Mutex
	isInstalled bool
)

// InitTray initializes the system tray icon and sets up message processing
func InitTray(cb TrayCallbacks) {
	trayMu.Lock()
	if isInstalled {
		trayMu.Unlock()
		return
	}
	callbacks = cb
	isInstalled = true
	trayMu.Unlock()

	go runMessageLoop()
}

func RemoveTray() {
	trayMu.Lock()
	defer trayMu.Unlock()
	if !isInstalled || trayHWnd == 0 {
		return
	}
	procShellNotifyIconW.Call(uintptr(NIM_DELETE), uintptr(unsafe.Pointer(&trayNID)))
	procDestroyWindow.Call(uintptr(trayHWnd))
	isInstalled = false
}

func runMessageLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := windows.UTF16PtrFromString("SnoopTrapTrayClass")

	wndClass := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   windows.NewCallback(wndProc),
		HInstance:     windows.Handle(hInst),
		LpszClassName: className,
	}

	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wndClass)))

	windowName, _ := windows.UTF16PtrFromString("SnoopTrapTrayWindow")
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		0, 0,
		hInst,
		0,
	)

	if hwnd == 0 {
		return
	}

	trayHWnd = windows.HWND(hwnd)

	// Load Icon: Try to extract from current executable, fallback to standard application icon
	exePath, _ := os.Executable()
	exePathW, _ := windows.UTF16PtrFromString(exePath)
	hIcon, _, _ := procExtractIconW.Call(hInst, uintptr(unsafe.Pointer(exePathW)), 0)
	if hIcon == 0 || hIcon == 1 {
		// Fallback to generic icon (32512 = IDI_APPLICATION)
		hIcon, _, _ = procLoadIconW.Call(0, 32512)
	}

	tipText := "SnoopTrap // IntruderLens Guardian"
	tipUTF16, _ := windows.UTF16FromString(tipText)

	trayNID = NOTIFYICONDATAW{
		CbSize:           uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:             trayHWnd,
		UID:              1,
		UFlags:           NIF_MESSAGE | NIF_ICON | NIF_TIP,
		UCallbackMessage: WM_TRAYICON,
		HIcon:            windows.Handle(hIcon),
	}
	copy(trayNID.SzTip[:], tipUTF16)

	procShellNotifyIconW.Call(uintptr(NIM_ADD), uintptr(unsafe.Pointer(&trayNID)))

	var msg MSG
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func wndProc(hwnd windows.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_TRAYICON:
		switch lParam {
		case WM_LBUTTONUP, WM_LBUTTONDBLCLK:
			if callbacks.OnShow != nil {
				go callbacks.OnShow()
			}
		case WM_RBUTTONUP:
			showContextMenu(hwnd)
		}
		return 0

	case WM_COMMAND:
		cmdID := uint32(wParam & 0xFFFF)
		switch cmdID {
		case CMD_SHOW:
			if callbacks.OnShow != nil {
				go callbacks.OnShow()
			}
		case CMD_ARM:
			if callbacks.OnArm != nil {
				go callbacks.OnArm()
			}
		case CMD_DISARM:
			if callbacks.OnDisarm != nil {
				go callbacks.OnDisarm()
			}
		case CMD_QUIT:
			if callbacks.OnQuit != nil {
				go callbacks.OnQuit()
			}
		}
		return 0

	case WM_DESTROY:
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func showContextMenu(hwnd windows.HWND) {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	openStr, _ := windows.UTF16PtrFromString("🖥️  Buka SnoopTrap")
	armStr, _ := windows.UTF16PtrFromString("🛡️  Aktifkan Siaga (/arm)")
	disarmStr, _ := windows.UTF16PtrFromString("🔓  Matikan Siaga (/disarm)")
	quitStr, _ := windows.UTF16PtrFromString("❌  Keluar dari SnoopTrap (Quit)")

	procAppendMenuW.Call(hMenu, uintptr(MF_STRING), uintptr(CMD_SHOW), uintptr(unsafe.Pointer(openStr)))
	procAppendMenuW.Call(hMenu, uintptr(MF_SEPARATOR), 0, 0)
	procAppendMenuW.Call(hMenu, uintptr(MF_STRING), uintptr(CMD_ARM), uintptr(unsafe.Pointer(armStr)))
	procAppendMenuW.Call(hMenu, uintptr(MF_STRING), uintptr(CMD_DISARM), uintptr(unsafe.Pointer(disarmStr)))
	procAppendMenuW.Call(hMenu, uintptr(MF_SEPARATOR), 0, 0)
	procAppendMenuW.Call(hMenu, uintptr(MF_STRING), uintptr(CMD_QUIT), uintptr(unsafe.Pointer(quitStr)))

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	procSetForeground.Call(uintptr(hwnd))
	procTrackPopupMenu.Call(
		hMenu,
		uintptr(TPM_RIGHTBUTTON|TPM_BOTTOMALIGN),
		uintptr(pt.X),
		uintptr(pt.Y),
		0,
		uintptr(hwnd),
		0,
	)
	procPostMessageW.Call(uintptr(hwnd), WM_NULL, 0, 0)
}

