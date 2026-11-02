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
	restoredFromReboot bool
	lastDisarmAt       time.Time

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

// NewApp creates a new App application struct and restores any persisted security state
func NewApp() *App {
	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = config.DefaultConfig()
	}

	persisted := config.LoadState()
	initialStatus := "DISARMED"
	initialOTP := ""
	restored := false

	if persisted.Status == "TRIGGERED" {
		initialStatus = "TRIGGERED"
		initialOTP = persisted.CurrentOTP
		if initialOTP == "" {
			initialOTP = telegram.GenerateOTP()
		}
		if persisted.RoomID != "" {
			stream.SetCurrentRoomID(persisted.RoomID)
		}
		restored = true
	} else if persisted.Status == "ARMED" {
		initialStatus = "ARMED"
		restored = true
	}

	return &App{
		cfg:                cfg,
		status:             initialStatus,
		currentOTP:         initialOTP,
		restoredFromReboot: restored,
		logs:               make([]string, 0),
	}
}

// InitialStatus returns the startup state before Wails initializes the window
func (a *App) InitialStatus() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// persistState flushes the current security status & OTP synchronously to disk so forced power-offs cannot clear the trap
func (a *App) persistState() {
	a.mu.RLock()
	st := &config.RuntimeState{
		Status:     a.status,
		CurrentOTP: a.currentOTP,
		RoomID:     stream.GetCurrentRoomID(),
		UpdatedAt:  time.Now().Unix(),
	}
	a.mu.RUnlock()

	if err := config.SaveState(st); err != nil {
		println("[STATE] Failed to persist state:", err.Error())
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.addLog("[INIT] SnoopTrap IntruderLens Guardian initialized.")
	a.addLog(fmt.Sprintf("[CONFIG] Idle Threshold: %ds | Target Audio: %s", a.cfg.IdleThresholdSeconds, a.cfg.TargetAudioDeviceGUID))

	// Register in Windows Startup (HKCU\...\Run) with 0ms startup delay so forced reboots always re-engage SnoopTrap
	if err := failsafe.EnsureWindowsStartup(); err != nil {
		a.addLog(fmt.Sprintf("[WARN] Windows Startup registration warning: %v", err))
	} else {
		a.addLog("[SYSTEM] Windows Auto-Startup registered (persistent across reboots).")
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
			if a.status == "TRIGGERED" {
				a.mu.Unlock()
				a.addLog("[SECURITY] System tray quit attempt blocked during Lockdown!")
				go failsafe.ForceFullscreenLockbox()
				return
			}
			a.isQuitting = true
			a.mu.Unlock()
			tray.RemoveTray()
			if a.ctx != nil {
				wailsRuntime.Quit(a.ctx)
			}
		},
	})

	// Restore previous status if laptop was forcibly powered off / restarted!
	a.mu.Lock()
	initialState := a.status
	otp := a.currentOTP
	botToken := a.cfg.TelegramBotToken
	chatID := a.cfg.TelegramChatID
	a.mu.Unlock()

	if initialState == "TRIGGERED" {
		a.addLog("[SECURITY] PERSISTENT LOCKDOWN RESTORED AFTER FORCED REBOOT! Re-engaging PIN Lockbox...")
		wailsRuntime.WindowShow(a.ctx)
		wailsRuntime.WindowSetAlwaysOnTop(a.ctx, true)
		wailsRuntime.WindowFullscreen(a.ctx)
		hook.EnableKeyboardBlocker()
		failsafe.StartLockdownWatchdog()

		roomID := a.GetStreamRoomID()
		a.persistState()

		// Emit breach-triggered once frontend WebView2 finishes mounting
		go func(rID, activeOTP string) {
			time.Sleep(800 * time.Millisecond)
			if a.ctx != nil {
				wailsRuntime.EventsEmit(a.ctx, "breach-triggered", map[string]interface{}{
					"otp":  activeOTP,
					"room": rID,
				})
			}
		}(roomID, otp)

		// Alert owner on Telegram (retry up to 10 times every 3s while laptop Wi-Fi reconnects after boot)
		if botToken != "" && chatID != "" {
			go func(activeOTP string) {
				liveURL := a.GetStreamURL()
				tg := telegram.NewClient(botToken, chatID)
				msg := fmt.Sprintf("🚨 <b>LAPTOP DINYALAKAN ULANG SAAT TERKUNCI (FORCED REBOOT)!</b>\n\nSeseorang mematikan paksa & menyalakan ulang laptop saat SnoopTrap terkunci! Tampilan kuncian PIN otomatis diaktifkan kembali!\n\n🔑 <b>KODE OTP AKTIF:</b> <code>%s</code>\n\n📡 <b>LIVE STREAM KAMERA:</b>\n%s\n\n🔇 <i>Mode Senyap aktif (alarm mati secara default). Tekan tombol di bawah jika ingin membunyikan sirine 100%%!</i>", activeOTP, liveURL)
				for attempt := 0; attempt < 10; attempt++ {
					a.mu.RLock()
					stillTriggered := a.status == "TRIGGERED"
					a.mu.RUnlock()
					if !stillTriggered {
						return
					}
					if tg.SendBreachAlertMessage(msg) == nil {
						return
					}
					time.Sleep(3 * time.Second)
				}
			}(otp)
		}
	} else if initialState == "ARMED" {
		curX, curY, _ := idle.GetCursorPos()
		tick, _ := idle.GetLastInputTick()
		a.mu.Lock()
		a.armedAt = time.Now()
		a.lastInputTickAtArm = tick
		a.initialCursorX = curX
		a.initialCursorY = curY
		a.prevArmedCursorX = curX
		a.prevArmedCursorY = curY
		a.lastArmedActivityAt = time.Now()
		a.mu.Unlock()
		a.addLog("[ARMED] Persistent ARMED state restored after reboot! Guarding laptop...")

		if botToken != "" && chatID != "" {
			go func() {
				tg := telegram.NewClient(botToken, chatID)
				msg := "🛡️ <b>Laptop Dinyalakan Ulang — Mode Siaga (ARMED) Dipertahankan!</b>\nSnoopTrap otomatis melanjutkan status SIAGA sebelumnya. Jika mouse/keyboard digunakan, jebakan langsung aktif!"
				for attempt := 0; attempt < 8; attempt++ {
					a.mu.RLock()
					stillArmed := a.status == "ARMED"
					a.mu.RUnlock()
					if !stillArmed {
						return
					}
					if tg.SendMessageWithInlineButton(msg, "🔓 SAYA SENDIRI (DISARM)", "disarm") == nil {
						return
					}
					time.Sleep(3 * time.Second)
				}
			}()
		}
	} else if a.cfg.AutoArm {
		a.addLog("[GUARD] Auto-arming active. Monitoring user idle time...")
	}

	// Start background idle monitoring
	go a.startIdleMonitor()

	// Start local streaming viewer server
	go stream.StartStreamServer()

	// Start persistent Telegram remote listener (allows instant /arm, /disarm, /alarm, VN broadcast anytime)
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
		func() {
			a.addLog("[REMOTE] Alarm siren triggered from Telegram! Maximizing volume to 100%...")
			audio.MaximizeMasterVolume()
			if a.ctx != nil {
				wailsRuntime.EventsEmit(a.ctx, "play-alarm-siren", nil)
			}
			audio.PlayAlarmSiren()
		},
		func() {
			a.addLog("[REMOTE] Alarm siren stopped from Telegram.")
			if a.ctx != nil {
				wailsRuntime.EventsEmit(a.ctx, "stop-alarm-siren", nil)
			}
			audio.StopAlarm()
		},
		func() {
			a.addLog("[REMOTE] Photo capture requested from Telegram! Emitting capture-photo-now...")
			if a.ctx != nil {
				wailsRuntime.EventsEmit(a.ctx, "capture-photo-now", nil)
			}
		},
		func() {
			a.addLog("[REMOTE] Audio recording requested from Telegram! Emitting record-audio-now...")
			if a.ctx != nil {
				wailsRuntime.EventsEmit(a.ctx, "record-audio-now", nil)
			}
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
			lastDisarm := a.lastDisarmAt
			a.mu.RUnlock()

			// 1. Auto arm when disarmed and idle long enough.
			// Cooldown: the low-level keyboard hook swallows keys during a TRIGGERED
			// lockdown, so Windows' GetLastInputInfo (and therefore idleSecs) stays stale
			// right after a disarm. Require a full fresh idle period measured from the
			// disarm moment so we don't instantly re-arm and re-trigger the PIN lockdown.
			if autoArm && currentState == "DISARMED" && idleSecs >= threshold {
				if !lastDisarm.IsZero() && time.Since(lastDisarm) < time.Duration(threshold)*time.Second {
					continue
				}
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

	a.persistState()
	a.addLog("[ARMED] Guardian trap is now active. Guarding laptop (filtering bumps/shifts)...")
	wailsRuntime.EventsEmit(a.ctx, "status-change", "ARMED")

	// Ensure background telegram listener is running
	a.ensureTelegramListener()

	// Send notification message to Telegram with full /help commands guide & disarm button
	if botToken != "" && chatID != "" {
		go func() {
			tg := telegram.NewClient(botToken, chatID)
			msg := `🛡️ <b>SnoopTrap Siaga Aktif!</b>
Laptop Anda sedang dijaga penuh (Mode Senyap). Jika mouse/keyboard digunakan oleh orang lain, kuncian layar & rekaman bukti kamera langsung terpicu tanpa suara!

📋 <b>Panduan Perintah Telegram:</b>
🛡️ <b>/arm</b> — Aktifkan mode siaga
🔓 <b>/disarm</b> — Matikan siaga / buka kuncian
📸 <b>/foto</b> — Ambil foto penyusup terbaru via webcam
🎙️ <b>/suara</b> — Rekam suara penyusup diam-diam (10 detik)
🔊 <b>/alarm</b> — Bunyikan sirine keras (volume 100%)
🔇 <b>/stopalarm</b> — Matikan bunyi sirine
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
	a.lastDisarmAt = time.Now()
	// Reset intentional-activity accumulators so a stale burst can't instantly re-trigger
	a.suspiciousActivityStartedAt = time.Time{}
	a.consecutiveInputTicks = 0
	a.armedKeypressCount = 0
	a.mu.Unlock()

	a.persistState()
	hook.DisableKeyboardBlocker()
	audio.StopAlarm()

	// Exit fullscreen & topmost and dismiss window back to background/tray
	failsafe.StopLockdownWatchdog()
	wailsRuntime.WindowSetAlwaysOnTop(a.ctx, false)
	wailsRuntime.WindowUnfullscreen(a.ctx)
	failsafe.RestoreNormalWindow()
	wailsRuntime.WindowSetSize(a.ctx, 1024, 768)
	wailsRuntime.WindowCenter(a.ctx)
	wailsRuntime.WindowHide(a.ctx)

	a.addLog("[DISARMED] Identity verified. SnoopTrap disarmed and window dismissed to tray.")
	wailsRuntime.EventsEmit(a.ctx, "status-change", "DISARMED")
	return true
}

// TriggerTrap initiates the lockbox trap in stealth silent mode (alarm muted unless triggered via Telegram)
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

	// Consistent stream room & URL + immediately flush TRIGGERED state & OTP to disk (survives forced power-off!)
	roomID := a.GetStreamRoomID()
	a.persistState()

	a.addLog("[BREACH] UNAUTHORIZED INPUT DETECTED! INITIATING SILENT STEALTH CAPTURE.")
	a.addLog("[STEALTH] Silent lockdown active. Siren muted by default (awaiting remote /alarm trigger from Telegram).")

	// 1. Immediately trap the intruder: Fullscreen, AlwaysOnTop, strict Keyboard Whitelist, and 50ms Win32 Lockdown Watchdog
	wailsRuntime.WindowShow(a.ctx)
	wailsRuntime.WindowSetAlwaysOnTop(a.ctx, true)
	wailsRuntime.WindowFullscreen(a.ctx)
	hook.EnableKeyboardBlocker()

	// Win32 Force Foreground, Task View/Taskbar Invisibility, Cursor Confinement & 50ms Watchdog
	failsafe.StartLockdownWatchdog()

	streamURL := a.GetStreamURL()

	// 2. IMMEDIATELY send Telegram alert with OTP, Live Stream Link, and Remote Alarm / Disarm buttons!
	if botToken != "" && chatID != "" {
		go func(otp string, liveURL string) {
			tg := telegram.NewClient(botToken, chatID)
			msg := fmt.Sprintf("🚨 <b>PERINGATAN PENYUSUP (BREACH DETECTED)!</b>\n\nLaptop Anda baru saja digunakan oleh seseorang!\n\n🔑 <b>KODE OTP:</b> <code>%s</code>\n\n📡 <b>LIVE STREAM KAMERA:</b>\n%s\n\n🔇 <i>Mode Senyap: Mengambil 3 foto & video 10 detik tanpa suara. Tekan <b>BUNYIKAN ALARM</b> di bawah jika ingin menyalakan sirine 100%%!</i>", otp, liveURL)
			for attempt := 0; attempt < 5; attempt++ {
				if tg.SendBreachAlertMessage(msg) == nil {
					return
				}
				time.Sleep(3 * time.Second)
			}
		}(currentOTP, streamURL)
	}

	// 3. Notify frontend to immediately capture 3 photos & start 10-second video recording in background!
	wailsRuntime.EventsEmit(a.ctx, "breach-triggered", map[string]interface{}{
		"otp":  currentOTP,
		"room": roomID,
	})

	// 4. Ensure telegram polling is active for remote commands / buttons
	a.ensureTelegramListener()
}

// ProcessIntruderCapture receives webcam frame from frontend and transmits to Telegram (with network retry for post-reboot Wi-Fi)
func (a *App) ProcessIntruderCapture(base64Image string, photoIndex int, totalPhotos int) bool {
	if totalPhotos <= 0 {
		totalPhotos = 3
	}
	if photoIndex <= 0 {
		photoIndex = 1
	}
	a.addLog(fmt.Sprintf("[WEBCAM] Silent intruder frame %d/%d captured via canvas.", photoIndex, totalPhotos))
	a.addLog(fmt.Sprintf("[NET] Dispatching photo %d/%d to Telegram Bot API...", photoIndex, totalPhotos))

	a.mu.RLock()
	botToken := a.cfg.TelegramBotToken
	chatID := a.cfg.TelegramChatID
	a.mu.RUnlock()

	tg := telegram.NewClient(botToken, chatID)
	caption := fmt.Sprintf("📸 <b>BUKTI FOTO PENYUSUP (%d/%d)</b>\nWajah & aktivitas penyusup di laptop.", photoIndex, totalPhotos)
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = tg.SendPhotoOnly(base64Image, caption)
		if err == nil {
			a.addLog(fmt.Sprintf("[NET] Telegram photo %d/%d transmitted successfully!", photoIndex, totalPhotos))
			return true
		}
		time.Sleep(3 * time.Second)
	}

	a.addLog(fmt.Sprintf("[NET] Telegram photo %d/%d transmit failed: %v", photoIndex, totalPhotos, err))
	return false
}

// ProcessIntruderVideo receives recorded video + audio from frontend and transmits to Telegram (with network retry)
func (a *App) ProcessIntruderVideo(videoBase64 string) bool {
	a.addLog(fmt.Sprintf("[REC] Intruder 10s video & audio received (size: %d bytes base64). Transmitting to Telegram...", len(videoBase64)))

	a.mu.RLock()
	botToken := a.cfg.TelegramBotToken
	chatID := a.cfg.TelegramChatID
	a.mu.RUnlock()

	tg := telegram.NewClient(botToken, chatID)
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = tg.SendVideo(videoBase64, "📹 <b>REKAMAN BUKTI PENYUSUP (10 DETIK VIDEO + AUDIO)</b>\nSistem merekam pergerakan & suara penyusup secara senyap.")
		if err == nil {
			a.addLog("[NET] Video & audio evidence transmitted successfully to Telegram!")
			return true
		}
		time.Sleep(3 * time.Second)
	}

	a.addLog(fmt.Sprintf("[NET] Telegram video dispatch failed: %v", err))
	return false
}

// ProcessIntruderAudio receives recorded mic audio from frontend and transmits to Telegram
func (a *App) ProcessIntruderAudio(audioBase64 string) bool {
	a.addLog(fmt.Sprintf("[MIC] Recorded intruder audio received (size: %d bytes base64). Transmitting to Telegram...", len(audioBase64)))

	a.mu.RLock()
	botToken := a.cfg.TelegramBotToken
	chatID := a.cfg.TelegramChatID
	a.mu.RUnlock()

	tg := telegram.NewClient(botToken, chatID)
	caption := "🎙️ <b>REKAMAN SUARA PENYUSUP (10 DETIK)</b>\nAudio direkam secara diam-diam dari mikrofon laptop."
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = tg.SendAudioFile(audioBase64, caption)
		if err == nil {
			a.addLog("[NET] Intruder voice recording transmitted successfully to Telegram!")
			return true
		}
		time.Sleep(2 * time.Second)
	}

	a.addLog(fmt.Sprintf("[NET] Telegram audio dispatch failed: %v", err))
	return false
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

	// ABSOLUTE LOCKDOWN: If system is TRIGGERED, completely block closing OR hiding the window!
	if st == "TRIGGERED" {
		a.addLog("[SECURITY] Close window attempt blocked! Absolute Lockdown active.")
		go failsafe.ForceFullscreenLockbox()
		return true // Prevent close & never hide!
	}

	// Explicit Quit requested from System Tray when NOT triggered
	if quitting {
		failsafe.StopLockdownWatchdog()
		tray.RemoveTray()
		return false // Allow normal exit
	}

	// Clicking [X] on Dashboard in DISARMED or ARMED mode hides window to System Tray so guardian stays active
	wailsRuntime.WindowHide(a.ctx)
	a.addLog("[SYSTEM] Window minimized to background/tray. SnoopTrap guardian active.")
	return true
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

// AddFrontendLog lets the WebView frontend push a diagnostic line into the shared log
// (used for PeerJS / streaming debugging, since the WebView console is not visible externally)
func (a *App) AddFrontendLog(msg string) {
	a.addLog(msg)
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
