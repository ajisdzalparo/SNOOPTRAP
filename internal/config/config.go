package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	TelegramBotToken           string `json:"telegramBotToken"`
	TelegramChatID             string `json:"telegramChatId"`
	IdleThresholdSeconds       int    `json:"idleThresholdSeconds"`
	TargetAudioDeviceGUID      string `json:"targetAudioDeviceGuid"`
	AutoArm                    bool   `json:"autoArm"`
	NuclearAction              string `json:"nuclearAction"` // "lock" or "taskkill"
	TargetKillProcess          string `json:"targetKillProcess"`
	VideoRecordDurationSeconds int    `json:"videoRecordDurationSeconds"`
	EmergencyMasterPIN         string `json:"emergencyMasterPin"`
	StreamServerHost           string `json:"streamServerHost"`
	StreamServerPort           string `json:"streamServerPort"`
	StreamRelayServerURL       string `json:"streamRelayServerUrl"`
	// WebRTC TURN relay (keeps media flowing when direct P2P fails behind NAT).
	// e.g. TurnURL "turn:139.190.98.100:3478". Left empty = STUN-only.
	TurnURL        string `json:"turnUrl"`
	TurnUsername   string `json:"turnUsername"`
	TurnCredential string `json:"turnCredential"`
}

func DefaultConfig() *Config {
	return &Config{
		TelegramBotToken:           "",
		TelegramChatID:             "",
		IdleThresholdSeconds:       180, // 3 minutes
		TargetAudioDeviceGUID:      "",
		AutoArm:                    true,
		NuclearAction:              "lock",
		TargetKillProcess:          "WhatsApp.exe",
		VideoRecordDurationSeconds: 10, // 10 seconds surveillance video + audio
		EmergencyMasterPIN:         "123456",
		StreamServerHost:           "",
		StreamServerPort:           "8080",
		StreamRelayServerURL:       "",
		TurnURL:                    "",
		TurnUsername:               "",
		TurnCredential:             "",
	}
}

func GetConfigDir() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	dir := filepath.Join(appData, "SnoopTrap")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func GetConfigPath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func LoadConfig() (*Config, error) {
	path, err := GetConfigPath()
	if err != nil {
		return DefaultConfig(), err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			_ = SaveConfig(cfg)
			return cfg, nil
		}
		return DefaultConfig(), err
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return DefaultConfig(), err
	}
	return cfg, nil
}

func SaveConfig(cfg *Config) error {
	path, err := GetConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// RuntimeState persists security status across forced power-offs or system reboots
type RuntimeState struct {
	Status     string `json:"status"`     // "DISARMED", "ARMED", "TRIGGERED"
	CurrentOTP string `json:"currentOtp"` // Active 6-digit OTP when TRIGGERED
	RoomID     string `json:"roomId"`     // Stream room ID
	UpdatedAt  int64  `json:"updatedAt"`
}

func GetStatePath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "state.json"), nil
}

func LoadState() *RuntimeState {
	defaultState := &RuntimeState{Status: "DISARMED"}
	path, err := GetStatePath()
	if err != nil {
		return defaultState
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return defaultState
	}
	var st RuntimeState
	if err := json.Unmarshal(data, &st); err != nil {
		return defaultState
	}
	if st.Status != "DISARMED" && st.Status != "ARMED" && st.Status != "TRIGGERED" {
		st.Status = "DISARMED"
	}
	return &st
}

// SaveState writes state to disk and calls f.Sync() immediately so forced power-off never loses state
func SaveState(st *RuntimeState) error {
	path, err := GetStatePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err == nil {
		_, writeErr := f.Write(data)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr == nil && syncErr == nil && closeErr == nil {
			if os.Rename(tmpPath, path) == nil {
				return nil
			}
		}
	}

	// Fallback direct synchronous write
	fDirect, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, writeErr := fDirect.Write(data)
	_ = fDirect.Sync()
	_ = fDirect.Close()
	return writeErr
}
