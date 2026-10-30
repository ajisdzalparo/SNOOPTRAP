package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

//go:embed snooptrap.exe
var binaryData []byte

var (
	user32Dll      = syscall.NewLazyDLL("user32.dll")
	procMessageBox = user32Dll.NewProc("MessageBoxW")
)

const (
	MB_OK          = 0x00000000
	MB_ICONINFO    = 0x00000040
	MB_YESNO       = 0x00000004
	IDYES          = 6
)

func showMessage(title, text string, flags uint32) int {
	tPtr, _ := syscall.UTF16PtrFromString(title)
	mPtr, _ := syscall.UTF16PtrFromString(text)
	r, _, _ := procMessageBox.Call(0, uintptr(unsafe.Pointer(mPtr)), uintptr(unsafe.Pointer(tPtr)), uintptr(flags))
	return int(r)
}

func main() {
	// Confirmation dialog
	btn := showMessage("SnoopTrap Setup", "Selamat datang di Installer SnoopTrap (IntruderLens)!\n\nApakah Anda ingin menginstal SnoopTrap ke laptop ini sekarang?", MB_YESNO|MB_ICONINFO)
	if btn != IDYES {
		return
	}

	// 1. Determine installation directory: %LocalAppData%\Programs\SnoopTrap
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		home, _ := os.UserHomeDir()
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	installDir := filepath.Join(localAppData, "Programs", "SnoopTrap")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		showMessage("Error", "Gagal membuat folder instalasi: "+err.Error(), MB_OK)
		return
	}

	targetExe := filepath.Join(installDir, "snooptrap.exe")

	// Terminate any running instance before overwriting
	_ = exec.Command("taskkill", "/IM", "snooptrap.exe", "/F").Run()

	// 2. Write embedded executable to disk
	if err := os.WriteFile(targetExe, binaryData, 0755); err != nil {
		showMessage("Error", "Gagal menyalin biner aplikasi: "+err.Error(), MB_OK)
		return
	}

	// 3. Create Desktop and Start Menu Shortcuts
	homeDir, _ := os.UserHomeDir()
	desktopDir := filepath.Join(homeDir, "Desktop")
	desktopLnk := filepath.Join(desktopDir, "SnoopTrap.lnk")

	appData := os.Getenv("APPDATA")
	startMenuDir := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs")
	startMenuLnk := filepath.Join(startMenuDir, "SnoopTrap.lnk")

	psScript := fmt.Sprintf(`
		$ws = New-Object -ComObject WScript.Shell
		$s1 = $ws.CreateShortcut('%s')
		$s1.TargetPath = '%s'
		$s1.WorkingDirectory = '%s'
		$s1.Description = 'SnoopTrap Intruder Detection System'
		$s1.Save()

		$s2 = $ws.CreateShortcut('%s')
		$s2.TargetPath = '%s'
		$s2.WorkingDirectory = '%s'
		$s2.Description = 'SnoopTrap Intruder Detection System'
		$s2.Save()
	`, desktopLnk, targetExe, installDir, startMenuLnk, targetExe, installDir)

	_ = exec.Command("powershell", "-NoProfile", "-Command", psScript).Run()

	// 4. Show success & launch application straight into settings!
	showMessage("Instalasi Berhasil", "SnoopTrap berhasil diinstal!\n\nShortcut telah dibuat di Desktop dan Start Menu.\nAplikasi sekarang akan otomatis dibuka ke menu Pengaturan (Settings).", MB_OK|MB_ICONINFO)

	cmd := exec.Command(targetExe)
	cmd.Dir = installDir
	_ = cmd.Start()
}
