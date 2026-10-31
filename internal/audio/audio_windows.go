package audio

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type AudioDevice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

var (
	winmmDll       = syscall.NewLazyDLL("winmm.dll")
	procPlaySound  = winmmDll.NewProc("PlaySoundW")
	user32Audio    = syscall.NewLazyDLL("user32.dll")
	procKeybd      = user32Audio.NewProc("keybd_event")
	sirenOnce      sync.Once
	sirenWavRemote []byte
)

const (
	SND_SYNC      = 0x0000
	SND_ASYNC     = 0x0001
	SND_NODEFAULT = 0x0002
	SND_MEMORY    = 0x0004
	SND_LOOP      = 0x0008

	VK_VOLUME_UP   = 0xAF
	VK_VOLUME_MUTE = 0xAD
)

// MaximizeMasterVolume aggressively unmutes and maximizes system volume without flooding the message loop
func MaximizeMasterVolume() {
	go func() {
		// Windows volume steps by 2% per VK_VOLUME_UP press.
		// 70 pulses guarantees reaching 100% volume even from 0% or muted state (70 * 2% = 140%).
		for i := 0; i < 70; i++ {
			procKeybd.Call(uintptr(VK_VOLUME_UP), 0, 0, 0)
			procKeybd.Call(uintptr(VK_VOLUME_UP), 0, 2, 0) // KEYEVENTF_KEYUP
			time.Sleep(1 * time.Millisecond)
		}
	}()
}

// GenerateSirenWav generates a high-urgency cyber siren WAV in memory
func GenerateSirenWav(durationSec int) []byte {
	sampleRate := 44100
	numSamples := sampleRate * durationSec
	numChannels := 1
	bitsPerSample := 16

	var buf bytes.Buffer

	// RIFF header
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+numSamples*2))
	buf.WriteString("WAVE")

	// fmt subchunk
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))               // Subchunk1Size
	binary.Write(&buf, binary.LittleEndian, uint16(1))                // AudioFormat (PCM)
	binary.Write(&buf, binary.LittleEndian, uint16(numChannels))      // NumChannels
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))       // SampleRate
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2))     // ByteRate
	binary.Write(&buf, binary.LittleEndian, uint16(numChannels*2))    // BlockAlign
	binary.Write(&buf, binary.LittleEndian, uint16(bitsPerSample))    // BitsPerSample

	// data subchunk
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(numSamples*2))

	// Generate siren audio: alternating 800Hz to 1800Hz square/sine alarm
	phase := 0.0
	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(sampleRate)
		// Frequency modulates between 800 Hz and 1800 Hz twice every second
		freq := 1200.0 + 600.0*math.Sin(2.0*math.Pi*3.0*t)
		phase += 2.0 * math.Pi * freq / float64(sampleRate)
		if phase > 2.0*math.Pi {
			phase -= 2.0 * math.Pi
		}

		sampleValue := math.Sin(phase)
		// Add some harsh harmonics for intimidation
		sampleValue += 0.3 * math.Sin(2.0*phase)
		if sampleValue > 1.0 {
			sampleValue = 1.0
		} else if sampleValue < -1.0 {
			sampleValue = -1.0
		}

		val := int16(sampleValue * 32000)
		binary.Write(&buf, binary.LittleEndian, val)
	}

	return buf.Bytes()
}

// PlayAlarmSiren unmutes, forces volume to 100%, and plays a 5-second high-urgency cyber alarm siren on remote command
func PlayAlarmSiren() {
	sirenOnce.Do(func() {
		sirenWavRemote = GenerateSirenWav(5)
	})
	MaximizeMasterVolume()
	if len(sirenWavRemote) > 0 {
		procPlaySound.Call(
			uintptr(unsafe.Pointer(&sirenWavRemote[0])),
			0,
			uintptr(SND_MEMORY|SND_ASYNC),
		)
	}
}

// StopAlarm stops any running sound
func StopAlarm() {
	procPlaySound.Call(0, 0, 0)
}

// GetAudioDevices lists system playback devices via Windows MMDevices registry
func GetAudioDevices() ([]AudioDevice, error) {
	cmd := exec.Command("powershell", "-NoProfile", "-Command", `
		$list = @()
		Get-ChildItem 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\MMDevices\Audio\Render' -ErrorAction SilentlyContinue | ForEach-Object {
			$propPath = Join-Path $_.PSPath 'Properties'
			if (Test-Path $propPath) {
				$p = Get-Item $propPath
				$type = $p.GetValue('{a45c254e-df1c-4efd-8020-67d146a850e0},2')
				$dev = $p.GetValue('{b3f8fa53-0004-438e-9003-51a46e139bfc},6')
				$state = (Get-ItemProperty $_.PSPath).'DeviceState'
				if (($type -or $dev) -and ($state -eq 1)) {
					$fullName = "$type ($dev)"
					$isSpeaker = ($fullName -match 'Speaker' -or $dev -match 'Realtek') -and ($fullName -notmatch 'Headphone|Headset|Cable|Virtual')
					$list += [PSCustomObject]@{
						id = $_.PSChildName
						name = $fullName
						isDefault = [bool]$isSpeaker
					}
				}
			}
		}
		if ($list.Count -gt 0) {
			$list | ConvertTo-Json -Compress
		}
	`)
	out, err := cmd.Output()
	if err == nil {
		var devices []AudioDevice
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			if strings.HasPrefix(trimmed, "[") {
				_ = json.Unmarshal([]byte(trimmed), &devices)
			} else if strings.HasPrefix(trimmed, "{") {
				var single AudioDevice
				if json.Unmarshal([]byte(trimmed), &single) == nil {
					devices = append(devices, single)
				}
			}
			if len(devices) > 0 {
				return devices, nil
			}
		}
	}

	// Fallback devices
	return []AudioDevice{
		{ID: "{70411993-6daa-4d9b-a367-c1d031d29d54}", Name: "Speakers (Realtek(R) Audio)", IsDefault: true},
		{ID: "default", Name: "Default System Speaker / Output", IsDefault: false},
	}, nil
}

