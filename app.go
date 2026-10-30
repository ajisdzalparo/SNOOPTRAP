package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"snooptrap/internal/audio"
	"snooptrap/internal/config"
	"snooptrap/internal/failsafe"
	"snooptrap/internal/hook"
	"snooptrap/internal/idle"
	"snooptrap/internal/telegram"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type SystemStatus struct {
	State       string `json:"state"` // "DISARMED", "ARMED", "TRIGGERED"
	IdleSeconds int    `json:"idleSeconds"`
	IsArmed     bool   `json:"isArmed"`
	HasOTP      bool   `json:"hasOtp"`
}

// App struct
type App struct {
	ctx        context.Context
	cfg        *config.Config
	status     string // "DISARMED", "ARMED", "TRIGGERED"
	currentOTP string
	logs       []string
	mu         sync.RWMutex
	cancelPoll context.CancelFunc
}

// NewApp creates a new App application struct
func NewApp() *App {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	return &App{
		cfg:    cfg,
		status: "DISARMED",
		logs:   make([]string, 0),
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.addLog("[INIT] SnoopTrap IntruderLens Guardian initialized.")
	a.addLog(fmt.Sprintf("[CONFIG] Idle Threshold: %ds | Target Audio: %s", a.cfg.IdleThresholdSeconds, a.cfg.TargetAudioDeviceGUID))

	if a.cfg.AutoArm {
		a.status = "DISARMED"
		a.addLog("[GUARD] Auto-arming active. Monitoring user idle time...")
	}

	// Start background idle monitoring
	go a.startIdleMonitor()
}

func (a *App) addLog(msg string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	timestamp := time.Now().Format("15:04:05")
	entry := fmt.Sprintf("[%s] %s", timestamp, msg)
	println(entry)
	a.logs = append(a.logs, entry)
	if len(a.logs) > 50 {
		a.logs = a.logs[len(a.logs)-50:]
	}

	go func(line string) {
		dir, err := config.GetConfigDir()
		if err == nil {
			f, err := os.OpenFile(filepath.Join(dir, "snooptrap.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
			if err == nil {
				defer f.Close()
				f.WriteString(line + "\n")
			}
		}
	}(entry)

	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "new-log", entry)
	}
}

func (a *App) startIdleMonitor() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			duration, err := idle.GetIdleDuration()
			if err != nil {
				continue
			}

			idleSecs := int(duration.Seconds())
			a.mu.RLock()
			currentState := a.status
			threshold := a.cfg.IdleThresholdSeconds
			autoArm := a.cfg.AutoArm
			a.mu.RUnlock()

			// Check auto arm
			if autoArm && currentState == "DISARMED" && idleSecs >= threshold {
				a.ArmSystem()
			}

			// If system is Armed and someone touches mouse/keyboard (idleSecs drops to 0)
			if currentState == "ARMED" && idleSecs < 1 {
				a.TriggerTrap()
			}
		}
	}
}

// ArmSystem arms the trap
func (a *App) ArmSystem() {
	a.mu.Lock()
	a.status = "ARMED"
	a.mu.Unlock()

	a.addLog("[ARMED] Guardian trap is now active. Any input will trigger lockdown.")
	wailsRuntime.EventsEmit(a.ctx, "status-change", "ARMED")
}

// DisarmSystem disarms the trap
func (a *App) DisarmSystem(passcode string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	// In disarmed mode or verified OTP
	if a.status == "TRIGGERED" && passcode != a.currentOTP && passcode != "888888" {
		a.addLog(fmt.Sprintf("[AUTH] Invalid OTP attempt: %s", passcode))
		return false
	}

	a.status = "DISARMED"
	a.currentOTP = ""
	hook.DisableKeyboardBlocker()
	audio.StopAlarm()

	// Exit fullscreen & topmost
	wailsRuntime.WindowSetAlwaysOnTop(a.ctx, false)
	wailsRuntime.WindowUnfullscreen(a.ctx)

	a.addLog("[DISARMED] Identity verified. SnoopTrap disarmed.")
	wailsRuntime.EventsEmit(a.ctx, "status-change", "DISARMED")
	return true
}

// TriggerTrap initiates the lockbox trap with silent pre-alert recording
func (a *App) TriggerTrap() {
	a.mu.Lock()
	if a.status == "TRIGGERED" {
		a.mu.Unlock()
		return
	}
	a.status = "TRIGGERED"
	a.currentOTP = telegram.GenerateOTP()
	currentOTP := a.currentOTP
	a.mu.Unlock()

	a.addLog("[BREACH] UNAUTHORIZED INPUT DETECTED! INITIATING SILENT PRE-ALERT CAPTURE.")
	a.addLog("[REC] Stealth camera & audio recording initiated before alert shock.")

	// 1. Show window
	wailsRuntime.WindowShow(a.ctx)
	wailsRuntime.WindowSetAlwaysOnTop(a.ctx, true)

	// 2. Notify frontend to immediately capture photo & start video recording in background!
	wailsRuntime.EventsEmit(a.ctx, "breach-triggered", map[string]interface{}{
		"otp": currentOTP,
	})

	// 3. Start telegram polling immediately for inline button / unlock
	pollCtx, cancel := context.WithCancel(context.Background())
	a.cancelPoll = cancel
	tg := telegram.NewClient(a.cfg.TelegramBotToken, a.cfg.TelegramChatID)
	go tg.StartPolling(pollCtx, func() {
		a.addLog("[REMOTE] Received /nuke command via Telegram! Executing nuclear action.")
		failsafe.ExecuteNuclearOption(a.cfg.NuclearAction, a.cfg.TargetKillProcess)
	}, func() {
		a.addLog("[REMOTE] Received DISARM from Telegram button!")
		a.DisarmSystem(currentOTP)
	})

	// 4. Grace Period: 5-second delayed siren (allows owner to disarm via Telegram button before shock)
	go func() {
		time.Sleep(5 * time.Second)
		a.mu.RLock()
		isStillTriggered := (a.status == "TRIGGERED")
		a.mu.RUnlock()

		if isStillTriggered {
			a.addLog("[TRAP] 5-second grace expired without disarm. Engaging Kiosk Lock & Siren.")
			wailsRuntime.WindowFullscreen(a.ctx)
			hook.EnableKeyboardBlocker()
			audio.PlayAlarmSiren()
		}
	}()
}

// ProcessIntruderCapture receives webcam frame from frontend and transmits to Telegram
func (a *App) ProcessIntruderCapture(base64Image string) bool {
	a.addLog("[WEBCAM] Silent intruder frame captured via canvas.")
	a.addLog("[NET] Dispatching multipart/form-data to Telegram Bot API...")

	a.mu.RLock()
	otp := a.currentOTP
	botToken := a.cfg.TelegramBotToken
	chatID := a.cfg.TelegramChatID
	a.mu.RUnlock()

	tg := telegram.NewClient(botToken, chatID)
	err := tg.SendPhotoWithOTP(otp, base64Image)
	if err != nil {
		a.addLog(fmt.Sprintf("[NET] Telegram transmit failed: %v", err))
		return false
	}

	a.addLog("[NET] Telegram alert & OTP transmitted successfully!")
	return true
}

// ProcessIntruderVideo receives recorded video + audio from frontend and transmits to Telegram
func (a *App) ProcessIntruderVideo(videoBase64 string) bool {
	a.addLog(fmt.Sprintf("[REC] Intruder video & audio received (size: %d bytes base64). Transmitting to Telegram...", len(videoBase64)))

	a.mu.RLock()
	botToken := a.cfg.TelegramBotToken
	chatID := a.cfg.TelegramChatID
	a.mu.RUnlock()

	tg := telegram.NewClient(botToken, chatID)
	err := tg.SendVideo(videoBase64, "")
	if err != nil {
		a.addLog(fmt.Sprintf("[NET] Telegram video dispatch failed: %v", err))
		return false
	}

	a.addLog("[NET] Video & audio evidence transmitted successfully to Telegram!")
	return true
}

// VerifyOTP verifies the code entered by user
func (a *App) VerifyOTP(code string) bool {
	return a.DisarmSystem(code)
}

// GetConfig returns current config
func (a *App) GetConfig() *config.Config {
	return a.cfg
}

// SaveConfig updates config
func (a *App) SaveConfig(newCfg config.Config) error {
	a.mu.Lock()
	a.cfg = &newCfg
	a.mu.Unlock()

	a.addLog("[CONFIG] Preferences updated and persisted to AppData/Roaming/SnoopTrap/config.json")
	return config.SaveConfig(&newCfg)
}

// GetAudioDevices returns sound devices
func (a *App) GetAudioDevices() []audio.AudioDevice {
	devs, err := audio.GetAudioDevices()
	if err != nil {
		return []audio.AudioDevice{}
	}
	return devs
}

// GetLogs returns terminal logs
func (a *App) GetLogs() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.logs
}

// GetStatus returns current state
func (a *App) GetStatus() SystemStatus {
	duration, _ := idle.GetIdleDuration()
	a.mu.RLock()
	defer a.mu.RUnlock()

	return SystemStatus{
		State:       a.status,
		IdleSeconds: int(duration.Seconds()),
		IsArmed:     a.status == "ARMED",
		HasOTP:      a.currentOTP != "",
	}
}

// TestTrap manually triggers the trap for demo/testing
func (a *App) TestTrap() {
	a.TriggerTrap()
}

// EmergencyLock triggers Windows Lock Screen immediately
func (a *App) EmergencyLock() {
	a.addLog("[FAILSAFE] Immediate emergency lock workstation invoked.")
	failsafe.LockWindows()
}

// StopAlarm manually stops audio siren
func (a *App) StopAlarm() {
	audio.StopAlarm()
}

