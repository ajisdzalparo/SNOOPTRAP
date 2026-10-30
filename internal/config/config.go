package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	TelegramBotToken       string `json:"telegramBotToken"`
	TelegramChatID         string `json:"telegramChatId"`
	IdleThresholdSeconds   int    `json:"idleThresholdSeconds"`
	TargetAudioDeviceGUID  string `json:"targetAudioDeviceGuid"`
	AutoArm                bool   `json:"autoArm"`
	NuclearAction          string `json:"nuclearAction"` // "lock" or "taskkill"
	TargetKillProcess      string `json:"targetKillProcess"`
}

func DefaultConfig() *Config {
	return &Config{
		TelegramBotToken:      "",
		TelegramChatID:        "",
		IdleThresholdSeconds:  180, // 3 minutes
		TargetAudioDeviceGUID: "",
		AutoArm:               true,
		NuclearAction:         "lock",
		TargetKillProcess:     "WhatsApp.exe",
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

