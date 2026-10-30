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
	procGetTickCount     = kernel32.NewProc("GetTickCount")
)

type LASTINPUTINFO struct {
	CbSize uint32
	DwTime uint32
}

// GetLastInputTick returns the raw tick (in ms) of the last mouse or keyboard input
func GetLastInputTick() (uint32, error) {
	var lii LASTINPUTINFO
	lii.CbSize = uint32(unsafe.Sizeof(lii))

	r1, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	if r1 == 0 {
		return 0, err
	}
	return lii.DwTime, nil
}

// GetIdleDuration returns how long the user has been idle (no keyboard/mouse input)
func GetIdleDuration() (time.Duration, error) {
	var lii LASTINPUTINFO
	lii.CbSize = uint32(unsafe.Sizeof(lii))

	r1, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	if r1 == 0 {
		return 0, err
	}

	rTick, _, _ := procGetTickCount.Call()
	currentTick := uint32(rTick)

	// In 32-bit unsigned math, (currentTick - DwTime) handles tick roll-over gracefully
	idleMillis := currentTick - lii.DwTime
	return time.Duration(idleMillis) * time.Millisecond, nil
}
