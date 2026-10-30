package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

//go:embed snooptrap.exe
var binaryData []byte

//go:embed uninstall.exe
var uninstallData []byte

var (
	user32Dll      = syscall.NewLazyDLL("user32.dll")
	procMessageBox = user32Dll.NewProc("MessageBoxW")
)

const (
	MB_OK       = 0x00000000
	MB_ICONINFO = 0x00000040
	MB_YESNO    = 0x00000004
	IDYES       = 6
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
	targetUninstall := filepath.Join(installDir, "uninstall.exe")

	// Terminate any running instance before overwriting
	_ = exec.Command("taskkill", "/IM", "snooptrap.exe", "/F").Run()

	// 2. Write embedded executables to disk
	if err := os.WriteFile(targetExe, binaryData, 0755); err != nil {
		showMessage("Error", "Gagal menyalin biner aplikasi: "+err.Error(), MB_OK)
		return
	}
	if len(uninstallData) > 0 {
		_ = os.WriteFile(targetUninstall, uninstallData, 0755)
	}

	// 3. Register in Windows Settings (Apps & Features / Installed Apps)
	k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall\SnoopTrap`, registry.ALL_ACCESS)
	if err == nil {
		_ = k.SetStringValue("DisplayName", "SnoopTrap")
		_ = k.SetStringValue("DisplayVersion", "1.1.0")
		_ = k.SetStringValue("Publisher", "SnoopTrap Security")
		_ = k.SetStringValue("DisplayIcon", targetExe)
		_ = k.SetStringValue("InstallLocation", installDir)
		_ = k.SetStringValue("UninstallString", fmt.Sprintf("\"%s\"", targetUninstall))
		k.Close()
	}

	// 4. Create Desktop and Start Menu Shortcuts
	homeDir, _ := os.UserHomeDir()
	desktopDir := filepath.Join(homeDir, "Desktop")
	desktopLnk := filepath.Join(desktopDir, "SnoopTrap.lnk")

	appData := os.Getenv("APPDATA")
	startMenuDir := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs")
	startMenuLnk := filepath.Join(startMenuDir, "SnoopTrap.lnk")
	startMenuUninstallLnk := filepath.Join(startMenuDir, "Uninstall SnoopTrap.lnk")

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

		$s3 = $ws.CreateShortcut('%s')
		$s3.TargetPath = '%s'
		$s3.WorkingDirectory = '%s'
		$s3.Description = 'Uninstall SnoopTrap'
		$s3.Save()
	`, desktopLnk, targetExe, installDir, startMenuLnk, targetExe, installDir, startMenuUninstallLnk, targetUninstall, installDir)

	_ = exec.Command("powershell", "-NoProfile", "-Command", psScript).Run()

	// 5. Show success & launch application straight into settings!
	showMessage("Instalasi Berhasil", "SnoopTrap berhasil diinstal!\n\n• Shortcut telah dibuat di Desktop dan Start Menu.\n• Aplikasi sekarang terdaftar di Windows Settings (Installed Apps) dan dapat di-uninstall kapan saja.\n\nAplikasi sekarang akan otomatis dibuka ke menu Pengaturan (Settings).", MB_OK|MB_ICONINFO)

	cmd := exec.Command(targetExe)
	cmd.Dir = installDir
	_ = cmd.Start()
}
