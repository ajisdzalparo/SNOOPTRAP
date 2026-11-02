// Typed Wails bridge interface
import { EventsOn } from '../../wailsjs/runtime/runtime';

export interface Config {
  telegramBotToken: string;
  telegramChatId: string;
  idleThresholdSeconds: number;
  targetAudioDeviceGuid: string;
  autoArm: boolean;
  nuclearAction: string;
  targetKillProcess: string;
  videoRecordDurationSeconds: number;
  emergencyMasterPin?: string;
  streamServerHost?: string;
  streamServerPort?: string;
  streamRelayServerUrl?: string;
}

export interface AudioDevice {
  id: string;
  name: string;
  isDefault: boolean;
}

export interface SystemStatus {
  state: 'DISARMED' | 'ARMED' | 'TRIGGERED';
  idleSeconds: number;
  isArmed: boolean;
  hasOtp: boolean;
}

declare global {
  interface Window {
    go?: {
      main: {
        App: {
          GetConfig: () => Promise<Config>;
          SaveConfig: (cfg: Config) => Promise<void>;
          GetAudioDevices: () => Promise<AudioDevice[]>;
          ArmSystem: () => Promise<void>;
          DisarmSystem: (passcode: string) => Promise<boolean>;
          VerifyOTP: (code: string) => Promise<boolean>;
          TriggerTrap: () => Promise<void>;
          ProcessIntruderCapture: (base64Image: string, photoIndex: number, totalPhotos: number) => Promise<boolean>;
          ProcessIntruderVideo: (base64Video: string) => Promise<boolean>;
          ProcessIntruderAudio: (base64Audio: string) => Promise<boolean>;
          GetLogs: () => Promise<string[]>;
          GetStatus: () => Promise<SystemStatus>;
          TestTrap: () => Promise<void>;
          EmergencyLock: () => Promise<void>;
          StopAlarm: () => Promise<void>;
          SetStreamRoomID: (room: string) => Promise<void>;
        };
      };
    };
    runtime?: {
      EventsOn: (eventName: string, callback: (...args: any[]) => void) => void;
      EventsEmit: (eventName: string, ...args: any[]) => void;
    };
  }
}

// Fallback mock for browser-only dev mode
const mockConfig: Config = {
  telegramBotToken: '',
  telegramChatId: '',
  idleThresholdSeconds: 180,
  targetAudioDeviceGuid: 'default',
  autoArm: true,
  nuclearAction: 'lock',
  targetKillProcess: 'WhatsApp.exe',
  videoRecordDurationSeconds: 120,
};

const mockLogs = [
  '[21:45:00] [SYSTEM] SnoopTrap Core engine booted.',
  '[21:45:01] [HOOK] Low-level keyboard hook ready (WH_KEYBOARD_LL)',
  '[21:45:01] [AUDIO] WASAPI audio override layer connected.',
  '[21:45:02] [TG_BOT] Gateway initialized.',
];

export const WailsBridge = {
  isWails: (): boolean => !!window.go?.main?.App,

  async getConfig(): Promise<Config> {
    if (window.go?.main?.App) {
      return await window.go.main.App.GetConfig();
    }
    return mockConfig;
  },

  async saveConfig(cfg: Config): Promise<void> {
    if (window.go?.main?.App) {
      return await window.go.main.App.SaveConfig(cfg);
    }
    console.log('Saved config (mock):', cfg);
  },

  async getAudioDevices(): Promise<AudioDevice[]> {
    if (window.go?.main?.App) {
      return await window.go.main.App.GetAudioDevices();
    }
    return [
      { id: 'default', name: 'Realtek Internal Speaker (Laptop)', isDefault: true },
      { id: 'bt_headset', name: 'Sony WH-1000XM4 (Bluetooth Headset)', isDefault: false },
    ];
  },

  async armSystem(): Promise<void> {
    if (window.go?.main?.App) {
      await window.go.main.App.ArmSystem();
    }
  },

  async disarmSystem(code: string): Promise<boolean> {
    if (window.go?.main?.App) {
      return await window.go.main.App.DisarmSystem(code);
    }
    return code === '123456' || code === '888888';
  },

  async verifyOTP(code: string): Promise<boolean> {
    if (window.go?.main?.App) {
      return await window.go.main.App.VerifyOTP(code);
    }
    return code === '123456' || code === '888888';
  },

  async triggerTrap(): Promise<void> {
    if (window.go?.main?.App) {
      await window.go.main.App.TriggerTrap();
    }
  },

  async processIntruderCapture(base64Image: string, photoIndex: number = 1, totalPhotos: number = 3): Promise<boolean> {
    if (window.go?.main?.App) {
      return await window.go.main.App.ProcessIntruderCapture(base64Image, photoIndex, totalPhotos);
    }
    console.log('Captured intruder image (mock, length):', base64Image.length);
    return true;
  },

  async processIntruderVideo(base64Video: string): Promise<boolean> {
    if (window.go?.main?.App?.ProcessIntruderVideo) {
      return await window.go.main.App.ProcessIntruderVideo(base64Video);
    }
    console.log('Captured intruder video (mock, length):', base64Video.length);
    return true;
  },

  async processIntruderAudio(base64Audio: string): Promise<boolean> {
    if (window.go?.main?.App?.ProcessIntruderAudio) {
      return await window.go.main.App.ProcessIntruderAudio(base64Audio);
    }
    console.log('Captured intruder audio (mock, length):', base64Audio.length);
    return true;
  },

  async getLogs(): Promise<string[]> {
    if (window.go?.main?.App) {
      return await window.go.main.App.GetLogs();
    }
    return mockLogs;
  },

  async getStatus(): Promise<SystemStatus> {
    if (window.go?.main?.App) {
      return await window.go.main.App.GetStatus();
    }
    return {
      state: 'DISARMED',
      idleSeconds: 42,
      isArmed: false,
      hasOtp: false,
    };
  },

  async emergencyLock(): Promise<void> {
    if (window.go?.main?.App) {
      await window.go.main.App.EmergencyLock();
    }
  },

  async setStreamRoomID(room: string): Promise<void> {
    if (window.go?.main?.App?.SetStreamRoomID) {
      await window.go.main.App.SetStreamRoomID(room);
    }
  },

  async getStreamWebSocketURL(): Promise<string> {
    if ((window as any).go?.main?.App?.GetStreamWebSocketURL) {
      return await (window as any).go.main.App.GetStreamWebSocketURL();
    }
    return '';
  },


  async playRemoteVoice(audioBase64: string, mimeType: string): Promise<void> {
    if ((window as any).go?.main?.App?.PlayRemoteVoice) {
      await (window as any).go.main.App.PlayRemoteVoice(audioBase64, mimeType);
    }
  },

  async testServerConnection(host: string, port: string): Promise<{ success: boolean; message: string }> {
    if ((window as any).go?.main?.App?.TestServerConnection) {
      return await (window as any).go.main.App.TestServerConnection(host, port);
    }
    return { success: false, message: 'Wails subsystem not connected.' };
  },

  onEvent(event: string, callback: (...args: any[]) => void): void {
    try {
      EventsOn(event, callback);
    } catch {
      const w = window as any;
      if (w.runtime?.EventsOnMultiple) {
        w.runtime.EventsOnMultiple(event, callback, -1);
      } else if (w.runtime?.EventsOn) {
        w.runtime.EventsOn(event, callback);
      }
    }
  },
};

