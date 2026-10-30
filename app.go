package main

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/http"
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
	logsMu             sync.Mutex
	mu                 sync.RWMutex
	cancelPoll         context.CancelFunc
	armedAt            time.Time
	lastInputTickAtArm uint32
	initialCursorX     int32
	initialCursorY     int32
	isAutoArmed        bool
	isQuitting         bool

	// Intentional Activity Detection (Filters accidental desk bumps / shifts)
	armedKeypressCount          int
	armedMouseClickCount        int
	armedMouseMovedTicks        int
	armedTotalDistance          float64
	lastArmedActivityAt         time.Time
	prevArmedCursorX            int32
	prevArmedCursorY            int32
	suspiciousActivityStartedAt time.Time
	consecutiveInputTicks       int
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

	// Install physical hardware keypress detector (for intentional usage confirmation)
	hook.SetOnKeyPressCallback(func() {
		a.mu.Lock()
		if a.status == "ARMED" {
			now := time.Now()
			// If previous activity was more than 4 seconds ago, reset counter
			if time.Since(a.lastArmedActivityAt) > 4*time.Second {
				a.armedKeypressCount = 0
				a.armedMouseClickCount = 0
				a.armedMouseMovedTicks = 0
				a.armedTotalDistance = 0
			}
			a.armedKeypressCount++
			a.lastArmedActivityAt = now
			count := a.armedKeypressCount
			a.mu.Unlock()
			a.addLog(fmt.Sprintf("[GUARD] Physical keystroke detected (%d). Evaluating intentional activity...", count))
		} else {
			a.mu.Unlock()
		}
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
	timestamp := time.Now().Format("15:04:05")
	entry := fmt.Sprintf("[%s] %s", timestamp, msg)
	println(entry)

	a.logsMu.Lock()
	a.logs = append(a.logs, entry)
	if len(a.logs) > 50 {
		a.logs = a.logs[len(a.logs)-50:]
	}
	a.logsMu.Unlock()

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
	ticker := time.NewTicker(200 * time.Millisecond)
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

			// 1. Auto arm when disarmed and idle long enough
			if autoArm && currentState == "DISARMED" && idleSecs >= threshold {
				a.mu.Lock()
				a.isAutoArmed = true
				a.mu.Unlock()
				a.ArmSystem()
				continue
			}

			// 2. If system is Armed, monitor for confirmed intentional laptop usage (sustained over 3 seconds)
			if currentState == "ARMED" {
				curX, curY, _ := idle.GetCursorPos()
				isMouseClicked := idle.IsMouseButtonPressed()

				// Measure cursor displacement since last sample
				stepDx := float64(curX - a.prevArmedCursorX)
				stepDy := float64(curY - a.prevArmedCursorY)
				stepDist := math.Hypot(stepDx, stepDy)
				a.prevArmedCursorX = curX
				a.prevArmedCursorY = curY

				hasActiveMovement := stepDist > 25.0
				now := time.Now()

				a.mu.Lock()
				hasKeystrokes := a.armedKeypressCount > 0
				hasInputNow := hasActiveMovement || isMouseClicked || hasKeystrokes

				if hasInputNow {
					if a.suspiciousActivityStartedAt.IsZero() {
						a.suspiciousActivityStartedAt = now
						a.consecutiveInputTicks = 1
					} else {
						a.consecutiveInputTicks++
					}
					a.lastArmedActivityAt = now
				} else {
					// If input stops for more than 1.5 seconds, reset accumulator!
					// (Accidental desk bumps or moving laptop stop within 1s)
					if !a.suspiciousActivityStartedAt.IsZero() && time.Since(a.lastArmedActivityAt) > 1500*time.Millisecond {
						a.suspiciousActivityStartedAt = time.Time{}
						a.consecutiveInputTicks = 0
						a.armedKeypressCount = 0
					}
				}

				activityDuration := time.Duration(0)
				if !a.suspiciousActivityStartedAt.IsZero() {
					activityDuration = time.Since(a.suspiciousActivityStartedAt)
				}
				ticks := a.consecutiveInputTicks
				kpCount := a.armedKeypressCount
				isAuto := a.isAutoArmed
				armedSince := time.Since(a.armedAt)
				a.mu.Unlock()

				// Require sustained, consecutive input over 3 full seconds!
				// Accidental bump or laptop relocation stops after 1s -> Cancelled!
				// Only active usage continuing for 3 full seconds will trigger the trap!
				if !a.suspiciousActivityStartedAt.IsZero() && activityDuration >= 3*time.Second && (ticks >= 8 || kpCount >= 3) {
					// Grace buffer for auto-arm
					if isAuto && armedSince < 4*time.Second {
						a.addLog("[GUARD] Activity detected within 4s of auto-arm. Resuming standby.")
						a.DisarmSystem("")
						continue
					}

					a.addLog(fmt.Sprintf("[BREACH] Confirmed intentional usage sustained for %.1fs (%d ticks, %d keys)! Triggering trap...", activityDuration.Seconds(), ticks, kpCount))
					a.TriggerTrap()
				}
			}
		}
	}
}

// ArmSystem arms the trap
func (a *App) ArmSystem() {
	curX, curY, _ := idle.GetCursorPos()
	tick, _ := idle.GetLastInputTick()

	a.mu.Lock()
	a.status = "ARMED"
	a.armedAt = time.Now()
	a.lastInputTickAtArm = tick
	a.initialCursorX = curX
	a.initialCursorY = curY
	a.prevArmedCursorX = curX
	a.prevArmedCursorY = curY
	a.armedKeypressCount = 0
	a.armedMouseClickCount = 0
	a.armedMouseMovedTicks = 0
	a.armedTotalDistance = 0
	a.suspiciousActivityStartedAt = time.Time{}
	a.consecutiveInputTicks = 0
	a.lastArmedActivityAt = time.Now()
	botToken := a.cfg.TelegramBotToken
	chatID := a.cfg.TelegramChatID
	a.mu.Unlock()

	a.addLog("[ARMED] Guardian trap is now active. Guarding laptop (filtering bumps/shifts)...")
	wailsRuntime.EventsEmit(a.ctx, "status-change", "ARMED")

	// Ensure background telegram listener is running
	a.ensureTelegramListener()

	// Send notification message to Telegram with full /help commands guide & disarm button
	if botToken != "" && chatID != "" {
		go func() {
			tg := telegram.NewClient(botToken, chatID)
			msg := `🛡️ <b>SnoopTrap Siaga Aktif!</b>
Laptop Anda sedang dijaga penuh. Jika mouse/keyboard digunakan oleh orang lain, alarm & live feed kamera langsung terpicu!

📋 <b>Panduan Perintah Telegram:</b>
🛡️ <b>/arm</b> — Aktifkan mode siaga
🔓 <b>/disarm</b> — Matikan siaga / buka kuncian
🔴 <b>/stream</b> — Tonton siaran langsung kamera & mic real-time
🎙️ <b>Kirim VN (Voice Note)</b> — Suara Anda diputar di speaker laptop (volume 100%)
🚨 <b>/nuke</b> — Kunci Windows darurat
📊 <b>/status</b> — Cek status laptop saat ini

<i>Saat Anda mau kembali ke meja, tap tombol di bawah:</i>`
			_ = tg.SendMessageWithInlineButton(msg, "🔓 SAYA SUDAH KEMBALI (DISARM)", "disarm")
		}()
	}
}

// DisarmSystem disarms the trap
func (a *App) DisarmSystem(passcode string) bool {
	a.mu.Lock()
	masterPIN := "123456"
	if a.cfg != nil && a.cfg.EmergencyMasterPIN != "" {
		masterPIN = a.cfg.EmergencyMasterPIN
	}

	// In disarmed mode or verified OTP
	// If passcode is empty (disarmed via Ctrl+Alt+U or Telegram button), or matches dynamic OTP, or matches user-configured Emergency Master PIN
	if a.status == "TRIGGERED" && passcode != "" && passcode != a.currentOTP && passcode != masterPIN {
		a.mu.Unlock()
		a.addLog(fmt.Sprintf("[AUTH] Invalid OTP attempt: %s", passcode))
		return false
	}

	a.status = "DISARMED"
	a.isAutoArmed = false
	a.currentOTP = ""
	a.mu.Unlock()

	hook.DisableKeyboardBlocker()
	audio.StopAlarm()

	// Exit fullscreen & topmost and dismiss window back to background/tray
	failsafe.RestoreNormalWindow()
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
	wailsRuntime.WindowSetAlwaysOnTop(a.ctx, true)
	wailsRuntime.WindowFullscreen(a.ctx)
	hook.EnableKeyboardBlocker()

	// Win32 Force Foreground & 100% Screen-Coverage over all apps/taskbar
	failsafe.ForceFullscreenLockbox()

	// Consistent WebRTC stream room & URL
	roomID := a.GetStreamRoomID()
	streamURL := a.GetStreamURL()

	// 2. IMMEDIATELY send Telegram alert with OTP and Live Stream Link (do NOT wait for webcam!)
	if botToken != "" && chatID != "" {
		go func(otp string, liveURL string) {
			tg := telegram.NewClient(botToken, chatID)
			msg := fmt.Sprintf("🚨 <b>PERINGATAN PENYUSUP (BREACH DETECTED)!</b>\n\nLaptop Anda baru saja digunakan oleh seseorang!\n\n🔑 <b>KODE OTP:</b> <code>%s</code>\n\n📡 <b>LIVE STREAM KAMERA:</b>\n%s\n\n<i>Klik link di atas atau tap tombol di bawah:</i>", otp, liveURL)
			_ = tg.SendMessageWithInlineButton(msg, "🔓 SAYA SENDIRI (DISARM)", "disarm")
		}(currentOTP, streamURL)
	}

	// 3. Notify frontend to immediately capture photo & start video recording in background!
	wailsRuntime.EventsEmit(a.ctx, "breach-triggered", map[string]interface{}{
		"otp":  currentOTP,
		"room": roomID,
	})

	// 4. Ensure telegram polling is active for remote commands / buttons
	a.ensureTelegramListener()

	// 5. Short 2-second grace period before sharp alert shock
	go func() {
		time.Sleep(2 * time.Second)
		a.mu.RLock()
		isStillTriggered := (a.status == "TRIGGERED")
		a.mu.RUnlock()

		if isStillTriggered {
			a.addLog("[TRAP] Grace expired without disarm. Engaging brief sharp alarm shock.")
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
	a.logsMu.Lock()
	defer a.logsMu.Unlock()
	copied := make([]string, len(a.logs))
	copy(copied, a.logs)
	return copied
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
	st := a.status
	quitting := a.isQuitting
	a.mu.RUnlock()

	// If system is in Lockdown (TRIGGERED), block closing the window completely!
	if st == "TRIGGERED" {
		a.addLog("[SECURITY] Close window attempt blocked. SnoopTrap is in Lockdown!")
		return true // Prevent close!
	}

	// In DISARMED mode or explicit quit requested, allow normal exit and clean up tray
	if quitting || st == "DISARMED" {
		tray.RemoveTray()
		return false // Allow normal close
	}

	// In ARMED mode: Hide window to System Tray
	wailsRuntime.WindowHide(a.ctx)
	a.addLog("[SYSTEM] Window minimized to background/tray. SnoopTrap guardian active.")
	return true // Prevent process exit so it continues guarding in background
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

type ServerTestResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func isIPv4(host string) bool {
	clean := strings.TrimPrefix(strings.TrimPrefix(host, "http://"), "https://")
	if idx := strings.Index(clean, ":"); idx != -1 {
		clean = clean[:idx]
	}
	clean = strings.Trim(clean, "/")
	return net.ParseIP(clean) != nil
}

func resolveStreamEndpoint(rawHost, rawPort string) string {
	host := strings.TrimSpace(rawHost)
	port := strings.TrimSpace(rawPort)
	if host == "" {
		return ""
	}

	cleanHost := strings.TrimPrefix(strings.TrimPrefix(host, "http://"), "https://")
	cleanHost = strings.TrimRight(cleanHost, "/")

	scheme := "http://"
	if strings.HasPrefix(host, "https://") {
		scheme = "https://"
	} else if strings.HasPrefix(host, "http://") {
		scheme = "http://"
	} else if !isIPv4(cleanHost) {
		// Domain name (e.g. stream-snooptrap.ajisdzalparo.com) -> use HTTPS by default
		scheme = "https://"
	}

	// If port is empty, 80, 443, or host already contains a port, don't append port
	if port == "" || port == "80" || port == "443" || strings.Contains(cleanHost, ":") {
		return fmt.Sprintf("%s%s", scheme, cleanHost)
	}

	return fmt.Sprintf("%s%s:%s", scheme, cleanHost, port)
}

// TestServerConnection tests connectivity to the configured stream relay server
func (a *App) TestServerConnection(host string, port string) ServerTestResult {
	baseURL := resolveStreamEndpoint(host, port)
	if baseURL == "" {
		return ServerTestResult{
			Success: false,
			Message: "IP atau domain server belum diisi.",
		}
	}

	healthURL := fmt.Sprintf("%s/health", strings.TrimRight(baseURL, "/"))
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(healthURL)
	if err != nil {
		return ServerTestResult{
			Success: false,
			Message: fmt.Sprintf("Gagal menghubungi server (%s): %v", healthURL, err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return ServerTestResult{
			Success: true,
			Message: fmt.Sprintf("Koneksi Berhasil! Relay Server Online di %s (HTTP 200 OK)", baseURL),
		}
	}

	return ServerTestResult{
		Success: false,
		Message: fmt.Sprintf("Server merespons HTTP %d (%s). Jika 502 Bad Gateway, pastikan container Docker sudah berjalan di VPS.", resp.StatusCode, resp.Status),
	}
}

// GetStreamRoomID returns the active WebRTC stream room ID or creates a persistent one
func (a *App) GetStreamRoomID() string {
	room := stream.GetCurrentRoomID()
	if room == "" {
		room = fmt.Sprintf("snoop-%d", time.Now().UnixNano()%1000000)
		stream.SetCurrentRoomID(room)
	}
	return room
}

// SetStreamRoomID updates the active WebRTC stream room ID
func (a *App) SetStreamRoomID(room string) {
	stream.SetCurrentRoomID(room)
}

// GetStreamURL returns the live streaming URL
func (a *App) GetStreamURL() string {
	room := a.GetStreamRoomID()
	if a.ctx != nil {
		wailsRuntime.EventsEmit(a.ctx, "start-stream-feed", room)
	}

	a.mu.RLock()
	relayURL := ""
	if a.cfg != nil {
		if strings.TrimSpace(a.cfg.StreamServerHost) != "" {
			relayURL = resolveStreamEndpoint(a.cfg.StreamServerHost, a.cfg.StreamServerPort)
		} else if strings.TrimSpace(a.cfg.StreamRelayServerURL) != "" {
			relayURL = strings.TrimSpace(a.cfg.StreamRelayServerURL)
		}
	}
	a.mu.RUnlock()

	if relayURL != "" {
		relayURL = strings.TrimRight(relayURL, "/")
		return fmt.Sprintf("%s/stream?room=%s", relayURL, room)
	}

	ip := stream.GetLocalIP()
	return fmt.Sprintf("http://%s:8765/stream?room=%s", ip, room)
}

// GetStreamWebSocketURL returns the WebSocket relay endpoint for the laptop to publish frames & receive audio
func (a *App) GetStreamWebSocketURL() string {
	room := a.GetStreamRoomID()
	a.mu.RLock()
	relayURL := ""
	if a.cfg != nil {
		if strings.TrimSpace(a.cfg.StreamServerHost) != "" {
			relayURL = resolveStreamEndpoint(a.cfg.StreamServerHost, a.cfg.StreamServerPort)
		} else if strings.TrimSpace(a.cfg.StreamRelayServerURL) != "" {
			relayURL = strings.TrimSpace(a.cfg.StreamRelayServerURL)
		}
	}
	a.mu.RUnlock()

	if relayURL != "" {
		relayURL = strings.TrimRight(relayURL, "/")
		wsURL := strings.Replace(strings.Replace(relayURL, "https://", "wss://", 1), "http://", "ws://", 1)
		return fmt.Sprintf("%s/ws?room=%s&role=host", wsURL, room)
	}

	return fmt.Sprintf("ws://127.0.0.1:8765/ws?room=%s&role=host", room)
}
