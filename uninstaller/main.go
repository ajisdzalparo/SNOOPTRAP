package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

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
	btn := showMessage("Uninstall SnoopTrap", "Apakah Anda yakin ingin menghapus (uninstall) SnoopTrap dari laptop ini?", MB_YESNO|MB_ICONINFO)
	if btn != IDYES {
		return
	}

	// 1. Terminate running snooptrap instance
	_ = exec.Command("taskkill", "/IM", "snooptrap.exe", "/F").Run()

	// 2. Remove shortcuts
	homeDir, _ := os.UserHomeDir()
	desktopLnk := filepath.Join(homeDir, "Desktop", "SnoopTrap.lnk")
	_ = os.Remove(desktopLnk)

	appData := os.Getenv("APPDATA")
	startMenuLnk := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "SnoopTrap.lnk")
	_ = os.Remove(startMenuLnk)

	// 3. Remove Windows Registry Uninstall Key
	_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall\SnoopTrap`)

	// 4. Remove installation files using self-deleting cmd script
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(homeDir, "AppData", "Local")
	}
	installDir := filepath.Join(localAppData, "Programs", "SnoopTrap")

	// Delete snooptrap.exe immediately
	_ = os.Remove(filepath.Join(installDir, "snooptrap.exe"))

	showMessage("Uninstall Selesai", "SnoopTrap telah berhasil dihapus sepenuhnya dari laptop Anda.", MB_OK|MB_ICONINFO)

	// Self-delete batch script to clean up uninstall.exe and the directory
	delBatch := filepath.Join(os.TempDir(), "snooptrap_cleanup.bat")
	batchContent := fmt.Sprintf(`@echo off
timeout /t 1 /nobreak > nul
rd /s /q "%s"
del "%%~f0"
`, installDir)
	_ = os.WriteFile(delBatch, []byte(batchContent), 0755)

	cmd := exec.Command("cmd.exe", "/C", delBatch)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
}

