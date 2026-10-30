package idle

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")
	procGetTickCount64   = kernel32.NewProc("GetTickCount64")
	procGetTickCount     = kernel32.NewProc("GetTickCount")
)

type LASTINPUTINFO struct {
	CbSize uint32
	DwTime uint32
}

// GetIdleDuration returns how long the user has been idle (no keyboard/mouse input)
func GetIdleDuration() (time.Duration, error) {
	var lii LASTINPUTINFO
	lii.CbSize = uint32(unsafe.Sizeof(lii))

	r1, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	if r1 == 0 {
		return 0, err
	}

	var currentTick uint64
	if procGetTickCount64.Find() == nil {
		rTick, _, _ := procGetTickCount64.Call()
		currentTick = uint64(rTick)
	} else {
		rTick, _, _ := procGetTickCount.Call()
		currentTick = uint64(rTick)
	}

	idleMillis := currentTick - uint64(lii.DwTime)
	return time.Duration(idleMillis) * time.Millisecond, nil
}
