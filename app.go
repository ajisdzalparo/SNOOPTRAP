package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"snooptrap/internal/audio"
	"snooptrap/internal/config"
	"snooptrap/internal/failsafe"
	"snooptrap/internal/hook"
	"snooptrap/internal/idle"
	"snooptrap/internal/stream"
	"snooptrap/internal/telegram"
	"snooptrap/internal/tray"

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
	ctx                context.Context
	cfg                *config.Config
	status             string // "DISARMED", "ARMED", "TRIGGERED"
	currentOTP         string
	logs               []string
	mu                 sync.RWMutex
	cancelPoll         context.CancelFunc
	armedAt            time.Time
	lastInputTickAtArm uint32
	initialCursorX     int32
	initialCursorY     int32
	isAutoArmed        bool
	isQuitting         bool
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

	// Install global hook for secret hotkey (Ctrl + Alt + U)
	hook.SetSecretDisarmCallback(func() {
		a.addLog("[SECRET_HOTKEY] Ctrl+Alt+U detected! Silently disarming SnoopTrap.")
		a.DisarmSystem("")
	})
	hook.InstallHook()

	// Initialize System Tray Icon (Show hidden icons)
	tray.InitTray(tray.TrayCallbacks{
		OnShow: func() {
			if a.ctx != nil {
				wailsRuntime.WindowShow(a.ctx)
				wailsRuntime.WindowUnminimise(a.ctx)
			}
		},
		OnArm: func() {
			a.ArmSystem()
		},
		OnDisarm: func() {
			a.DisarmSystem("")
		},
		OnQuit: func() {
			a.mu.Lock()
			a.isQuitting = true
			a.mu.Unlock()
			tray.RemoveTray()
			if a.ctx != nil {
				wailsRuntime.Quit(a.ctx)
			}
		},
	})

	// Start background idle monitoring
	go a.startIdleMonitor()

	// Start local streaming viewer server
	go stream.StartStreamServer()

	// Start persistent Telegram remote listener (allows instant /arm, /disarm, VN broadcast anytime)
	go a.ensureTelegramListener()
}

func (a *App) ensureTelegramListener() {
	a.mu.Lock()
	if a.cancelPoll != nil {
		a.mu.Unlock()
		return
	}
	pollCtx, cancel := context.WithCancel(context.Background())
	a.cancelPoll = cancel
	botToken := a.cfg.TelegramBotToken
	chatID := a.cfg.TelegramChatID
	a.mu.Unlock()

	if botToken == "" || chatID == "" {
		return
	}

	tg := telegram.NewClient(botToken, chatID)
	go tg.StartPolling(
		pollCtx,
		func() {
			a.addLog("[REMOTE] Received nuke command via Telegram.")
			failsafe.ExecuteNuclearOption(a.cfg.NuclearAction, a.cfg.TargetKillProcess)
		},
		func() {
			a.addLog("[REMOTE] Received DISARM from Telegram button/command!")
			a.DisarmSystem("")
		},
		func(audioBase64 string, mimeType string) {
			a.PlayRemoteVoice(audioBase64, mimeType)
		},
		func() {
			a.addLog("[REMOTE] Received /arm from Telegram! Arming system immediately.")
			a.ArmSystem()
		},
		func() string {
			a.mu.RLock()
			st := a.status
			otp := a.currentOTP
			a.mu.RUnlock()
			dur, _ := idle.GetIdleDuration()
			otpInfo := ""
			if st == "TRIGGERED" && otp != "" {
				otpInfo = fmt.Sprintf("\n• OTP Aktif: <code>%s</code>", otp)
			}
			return fmt.Sprintf("• Mode: <b>%s</b>\n• Idle Komputer: %d detik\n• Status Kuncian: %s%s", st, int(dur.Seconds()), map[bool]string{true: "Terkunci (Breach)", false: "Normal"}[st == "TRIGGERED"], otpInfo)
		},
		func() string {
			return a.GetStreamURL()
		},
	)
}

func (a *App) restartTelegramListener() {
	a.mu.Lock()
	if a.cancelPoll != nil {
		a.cancelPoll()
		a.cancelPoll = nil
	}
	a.mu.Unlock()
	a.ensureTelegramListener()
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
				_, _ = f.WriteString(line + "\n")
				_ = f.Close()
			}
		}
	}(entry)

	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "new-log", entry)
	}
}

func (a *App) startIdleMonitor() {
	ticker := time.NewTicker(300 * time.Millisecond)
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
			armedAt := a.armedAt
			lastTickAtArm := a.lastInputTickAtArm
			initX := a.initialCursorX
			initY := a.initialCursorY
			isAuto := a.isAutoArmed
			a.mu.RUnlock()

			// 1. Check auto arm when disarmed and idle long enough
			if autoArm && currentState == "DISARMED" && idleSecs >= threshold {
				a.mu.Lock()
				a.isAutoArmed = true
				a.mu.Unlock()
				a.ArmSystem()
				continue
			}

			// 2. If system is Armed, monitor for intentional mouse movement or key press
			if currentState == "ARMED" {
				curX, curY, _ := idle.GetCursorPos()
				dx := int(curX - initX)
				dy := int(curY - initY)
				distSq := dx*dx + dy*dy

				// Significant mouse movement (> 30 pixels distance)
				// Ignores optical sensor micro-jitter or desk bumps!
				isSignificantMouseMoved := distSq > 900

				currentTick, err := idle.GetLastInputTick()
				hasAnyInput := (err == nil && currentTick != lastTickAtArm && idleSecs < 2)

				// CASE A: Within the 3-second buffer after idle / arming
				if time.Since(armedAt) < 3*time.Second {
					if hasAnyInput {
						if isAuto {
							// If it was auto-armed and user touched laptop within 3s, they are still at the desk!
							// Smoothly return to standby without triggering alarm
							a.addLog("[GUARD] Activity detected within 3s of idle auto-arm. Resuming standby.")
							a.DisarmSystem("")
						} else {
							// Manual arm: update stabilization baseline so user lifting their hand doesn't trigger
							a.mu.Lock()
							a.lastInputTickAtArm = currentTick
							a.initialCursorX = curX
							a.initialCursorY = curY
							a.mu.Unlock()
						}
					}
					continue
				}

				// CASE B: System is fully HOT (after 3 seconds)
				// Triggers ONLY if:
				// - Significant mouse movement (> 30 pixels) OR
				// - An actual key is pressed on keyboard (distSq < 25 but hasAnyInput is true)
				if hasAnyInput {
					if isSignificantMouseMoved || distSq < 25 {
						a.TriggerTrap()
					}
				}
			}
		}
	}
}

// ArmSystem arms the trap
func (a *App) ArmSystem() {
	a.mu.Lock()
	a.status = "ARMED"
	a.armedAt = time.Now()
	tick, _ := idle.GetLastInputTick()
	a.lastInputTickAtArm = tick
	curX, curY, _ := idle.GetCursorPos()
	a.initialCursorX = curX
	a.initialCursorY = curY
	botToken := a.cfg.TelegramBotToken
	chatID := a.cfg.TelegramChatID
	a.mu.Unlock()

	a.addLog("[ARMED] Guardian trap is now active. Guarding laptop...")
	wailsRuntime.EventsEmit(a.ctx, "status-change", "ARMED")

	// Ensure background telegram listener is running
	a.ensureTelegramListener()

	// Send notification message to Telegram with interactive button
	if botToken != "" && chatID != "" {
		go func() {
			tg := telegram.NewClient(botToken, chatID)
			msg := "🛡️ <b>SnoopTrap Siaga: Laptop Anda sedang dijaga.</b>\n<i>(Belum ada yang menyentuh laptop).</i>\n\nSaat Anda mau kembali ke meja, tap tombol di bawah ini agar laptop bisa langsung dipakai tanpa alarm:"
			_ = tg.SendMessageWithInlineButton(msg, "🔓 SAYA SUDAH KEMBALI (DISARM)", "disarm")
		}()
	}
}

// DisarmSystem disarms the trap
func (a *App) DisarmSystem(passcode string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	masterPIN := a.cfg.EmergencyMasterPIN
	if masterPIN == "" {
		masterPIN = "123456"
	}

	// In disarmed mode or verified OTP
	// If passcode is empty (disarmed via Ctrl+Alt+U or Telegram button), or matches dynamic OTP, or matches user-configured Emergency Master PIN
	if a.status == "TRIGGERED" && passcode != "" && passcode != a.currentOTP && passcode != masterPIN {
		a.addLog(fmt.Sprintf("[AUTH] Invalid OTP attempt: %s", passcode))
		return false
	}

	a.status = "DISARMED"
	a.isAutoArmed = false
	a.currentOTP = ""
	hook.DisableKeyboardBlocker()
	audio.StopAlarm()

	// Exit fullscreen & topmost and dismiss window back to background/tray
	wailsRuntime.WindowSetAlwaysOnTop(a.ctx, false)
	wailsRuntime.WindowUnfullscreen(a.ctx)
	wailsRuntime.WindowSetSize(a.ctx, 1024, 768)
	wailsRuntime.WindowCenter(a.ctx)
	wailsRuntime.WindowHide(a.ctx)

	a.addLog("[DISARMED] Identity verified. SnoopTrap disarmed and window dismissed to tray.")
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
	botToken := a.cfg.TelegramBotToken
	chatID := a.cfg.TelegramChatID
	a.mu.Unlock()

	a.addLog("[BREACH] UNAUTHORIZED INPUT DETECTED! INITIATING SILENT PRE-ALERT CAPTURE.")
	a.addLog("[REC] Stealth camera & audio recording initiated before alert shock.")

	// 1. Immediately trap the intruder: Fullscreen, AlwaysOnTop, and BLOCK Windows key / Alt+Tab
	wailsRuntime.WindowShow(a.ctx)
	wailsRuntime.WindowFullscreen(a.ctx)
	wailsRuntime.WindowSetAlwaysOnTop(a.ctx, true)
	hook.EnableKeyboardBlocker()

	// 2. IMMEDIATELY send Telegram alert with OTP (do NOT wait for webcam!)
	if botToken != "" && chatID != "" {
		go func(otp string) {
			tg := telegram.NewClient(botToken, chatID)
			msg := fmt.Sprintf("🚨 <b>PERINGATAN PENYUSUP (BREACH DETECTED)!</b>\n\nMouse atau keyboard laptop Anda baru saja disentuh!\n\n🔑 <b>KODE OTP PEMBUKA:</b> <code>%s</code>\n\nMasukkan 6 digit angka di atas pada layar laptop, atau tap tombol di bawah:", otp)
			_ = tg.SendMessageWithInlineButton(msg, "🔓 SAYA SENDIRI (DISARM)", "disarm")
		}(currentOTP)
	}

	// 3. Notify frontend to immediately capture photo & start video recording in background!
	wailsRuntime.EventsEmit(a.ctx, "breach-triggered", map[string]interface{}{
		"otp": currentOTP,
	})

	// 4. Ensure telegram polling is active for remote commands / buttons
	a.ensureTelegramListener()

	// 5. Grace Period: 5-second delayed siren (allows owner to disarm silently via Telegram button or Ctrl+Alt+U)
	go func() {
		time.Sleep(5 * time.Second)
		a.mu.RLock()
		isStillTriggered := (a.status == "TRIGGERED")
		a.mu.RUnlock()

		if isStillTriggered {
			a.addLog("[TRAP] 5-second grace expired without disarm. Engaging Siren.")
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

	a.addLog("[NET] Telegram alert & photo transmitted successfully!")
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
	err := config.SaveConfig(&newCfg)
	a.restartTelegramListener()
	return err
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

// beforeClose is called when the user attempts to close the window
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	a.mu.RLock()
	isTriggered := (a.status == "TRIGGERED")
	quitting := a.isQuitting
	a.mu.RUnlock()

	// If system is in Lockdown (TRIGGERED), block closing the window completely!
	if isTriggered {
		a.addLog("[SECURITY] Close window attempt blocked. SnoopTrap is in Lockdown!")
		return true // Prevent close!
	}

	// If explicit quit requested from Tray menu, allow exit
	if quitting {
		tray.RemoveTray()
		return false
	}

	// Normal close ('X'): Hide window to System Tray (Show hidden icons)
	wailsRuntime.WindowHide(a.ctx)
	a.addLog("[SYSTEM] Window minimized to background/tray. SnoopTrap guardian active.")
	return true // Prevent process exit so it continues running in background
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

// PlayRemoteVoice broadcasts audio / voice note received from Telegram directly through laptop speakers
func (a *App) PlayRemoteVoice(audioBase64 string, mimeType string) {
	a.addLog(fmt.Sprintf("[MEGAPHONE] Remote voice note received from Telegram (type: %s)! Maximizing volume...", mimeType))
	audio.MaximizeMasterVolume()
	wailsRuntime.EventsEmit(a.ctx, "play-remote-audio", map[string]string{
		"audio": audioBase64,
		"mime":  mimeType,
	})
}

// SetStreamRoomID updates the active WebRTC stream room ID
func (a *App) SetStreamRoomID(room string) {
	stream.SetCurrentRoomID(room)
}

// GetStreamURL returns the live streaming URL
func (a *App) GetStreamURL() string {
	room := stream.GetCurrentRoomID()
	if room == "" {
		room = fmt.Sprintf("snoop-%d", time.Now().Unix()%1000000)
		stream.SetCurrentRoomID(room)
		wailsRuntime.EventsEmit(a.ctx, "start-stream-feed", room)
	}

	a.mu.RLock()
	relayURL := ""
	if a.cfg != nil {
		relayURL = a.cfg.StreamRelayServerURL
	}
	a.mu.RUnlock()

	if relayURL != "" {
		relayURL = strings.TrimRight(relayURL, "/")
		return fmt.Sprintf("%s/stream?room=%s", relayURL, room)
	}

	ip := stream.GetLocalIP()
	return fmt.Sprintf("http://%s:8765/stream?room=%s", ip, room)
}
