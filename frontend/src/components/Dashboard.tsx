import { useState, useEffect } from 'react';
import type { FC, FormEvent } from 'react';
import {
  Shield,
  ShieldAlert,
  Settings,
  Volume2,
  Clock,
  Send,
  Skull,
  Play,
  CheckCircle2,
  Terminal,
} from 'lucide-react';
import { WailsBridge } from '../services/wailsBridge';
import type { Config, AudioDevice, SystemStatus } from '../services/wailsBridge';

interface DashboardProps {
  onTriggerTrap: () => void;
  logs: string[];
}

export const Dashboard: FC<DashboardProps> = ({ onTriggerTrap, logs }) => {
  const [config, setConfig] = useState<Config>({
    telegramBotToken: '',
    telegramChatId: '',
    idleThresholdSeconds: 180,
    targetAudioDeviceGuid: 'default',
    autoArm: true,
    nuclearAction: 'lock',
    targetKillProcess: 'WhatsApp.exe',
    videoRecordDurationSeconds: 30,
    emergencyMasterPin: '123456',
    streamRelayServerUrl: '',
  });

  const [audioDevices, setAudioDevices] = useState<AudioDevice[]>([]);
  const [status, setStatus] = useState<SystemStatus>({
    state: 'DISARMED',
    idleSeconds: 0,
    isArmed: false,
    hasOtp: false,
  });

  const [activeTab, setActiveTab] = useState<'status' | 'config'>('config');
  const [saveSuccess, setSaveSuccess] = useState<boolean>(false);
  const [isSaving, setIsSaving] = useState<boolean>(false);

  // Load initial configuration and audio devices
  useEffect(() => {
    async function loadData() {
      try {
        const [loadedCfg, devs, currentStatus] = await Promise.all([
          WailsBridge.getConfig(),
          WailsBridge.getAudioDevices(),
          WailsBridge.getStatus(),
        ]);
        if (loadedCfg) setConfig(loadedCfg);
        if (devs) setAudioDevices(devs);
        if (currentStatus) setStatus(currentStatus);
      } catch (e) {
        console.error('Error loading dashboard data:', e);
      }
    }
    loadData();

    // Poll status periodically
    const interval = setInterval(async () => {
      try {
        const s = await WailsBridge.getStatus();
        setStatus(s);
      } catch {
        // ignore
      }
    }, 1000);

    return () => clearInterval(interval);
  }, []);

  const handleSaveConfig = async (e: FormEvent) => {
    e.preventDefault();
    setIsSaving(true);
    try {
      await WailsBridge.saveConfig(config);
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 3000);
    } catch (err) {
      console.error('Save failed:', err);
    } finally {
      setIsSaving(false);
    }
  };

  const handleArmToggle = async () => {
    if (status.isArmed) {
      await WailsBridge.disarmSystem('');
    } else {
      await WailsBridge.armSystem();
    }
    const s = await WailsBridge.getStatus();
    setStatus(s);
  };

  const handleEmergencyLock = async () => {
    if (confirm('Execute instant Windows Workstation Lock?')) {
      await WailsBridge.emergencyLock();
    }
  };

  return (
    <div className="flex flex-col h-screen bg-[#050505] text-white select-none font-mono">
      {/* Top Header */}
      <header className="flex justify-between items-center px-6 py-4 border-b border-[#222] bg-[#080808]">
        <div className="flex items-center gap-3">
          <div className="p-2 border border-[#FF003C] bg-[#FF003C]/10 text-[#FF003C]">
            <ShieldAlert className="w-5 h-5" />
          </div>
          <div>
            <h1 className="text-lg font-black tracking-widest text-[#FF003C]">
              SNOOPTRAP // COMMAND_HUB
            </h1>
            <p className="text-[10px] text-[#8F8F8F]">
              Personal Intruder Detection & Laptop Guardian
            </p>
          </div>
        </div>

        {/* Tab Switcher */}
        <div className="flex gap-2">
          <button
            onClick={() => setActiveTab('status')}
            className={`px-4 py-2 text-xs font-bold tracking-wider uppercase border transition-all cursor-pointer ${
              activeTab === 'status'
                ? 'border-[#FF003C] text-[#FF003C] bg-[#FF003C]/10'
                : 'border-[#333] text-[#8F8F8F] hover:text-white'
            }`}
          >
            STATUS & CONTROLS
          </button>
          <button
            onClick={() => setActiveTab('config')}
            className={`px-4 py-2 text-xs font-bold tracking-wider uppercase border transition-all cursor-pointer flex items-center gap-1.5 ${
              activeTab === 'config'
                ? 'border-[#FF003C] text-[#FF003C] bg-[#FF003C]/10'
                : 'border-[#333] text-[#8F8F8F] hover:text-white'
            }`}
          >
            <Settings className="w-3.5 h-3.5" />
            CONFIG
          </button>
        </div>
      </header>

      {/* Main Content Area */}
      <main className="flex-1 overflow-y-auto p-6 space-y-6">
        {activeTab === 'status' ? (
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            {/* Status Card */}
            <div className="md:col-span-2 border border-[#333] bg-[#0a0a0a] p-6 space-y-6 relative">
              <div className="flex justify-between items-center border-b border-[#222] pb-4">
                <span className="text-xs text-[#8F8F8F] tracking-widest uppercase">
                  GUARDIAN STATE
                </span>
                <span
                  className={`px-3 py-1 text-xs font-black tracking-widest uppercase border ${
                    status.isArmed
                      ? 'border-[#FF003C] text-[#FF003C] bg-[#FF003C]/10 animate-pulse'
                      : 'border-[#00FF41] text-[#00FF41] bg-[#00FF41]/10'
                  }`}
                >
                  {status.state}
                </span>
              </div>

              {/* Metrics */}
              <div className="grid grid-cols-2 sm:grid-cols-3 gap-4">
                <div className="border border-[#222] p-4 bg-[#050505]">
                  <div className="flex items-center gap-2 text-xs text-[#8F8F8F] mb-1">
                    <Clock className="w-3.5 h-3.5" />
                    <span>IDLE SECONDS</span>
                  </div>
                  <div className="text-2xl font-black text-white">
                    {status.idleSeconds}s
                  </div>
                  <div className="text-[10px] text-[#8F8F8F]">
                    Auto-arm threshold: {config.idleThresholdSeconds}s
                  </div>
                </div>

                <div className="border border-[#222] p-4 bg-[#050505]">
                  <div className="flex items-center gap-2 text-xs text-[#8F8F8F] mb-1">
                    <Volume2 className="w-3.5 h-3.5" />
                    <span>AUDIO OVERRIDE</span>
                  </div>
                  <div className="text-sm font-bold text-[#FF003C] truncate">
                    WASAPI TARGET
                  </div>
                  <div className="text-[10px] text-[#8F8F8F]">
                    Forces max laptop volume
                  </div>
                </div>

                <div className="border border-[#222] p-4 bg-[#050505]">
                  <div className="flex items-center gap-2 text-xs text-[#8F8F8F] mb-1">
                    <Send className="w-3.5 h-3.5" />
                    <span>TELEGRAM 2FA</span>
                  </div>
                  <div className="text-sm font-bold text-[#00FF41]">
                    {config.telegramBotToken ? 'CONFIGURED' : 'TOKEN MISSING'}
                  </div>
                  <div className="text-[10px] text-[#8F8F8F]">
                    {config.telegramChatId ? `Chat: ${config.telegramChatId}` : 'No chat ID'}
                  </div>
                </div>
              </div>

              {/* Action Buttons */}
              <div className="pt-2 flex flex-wrap gap-4">
                <button
                  onClick={handleArmToggle}
                  className={`flex-1 py-4 text-xs font-black tracking-widest uppercase border transition-all cursor-pointer flex items-center justify-center gap-2 ${
                    status.isArmed
                      ? 'border-[#8F8F8F] text-[#8F8F8F] hover:bg-white/10'
                      : 'border-[#FF003C] text-[#FF003C] bg-[#FF003C]/10 hover:bg-[#FF003C] hover:text-[#050505]'
                  }`}
                >
                  <Shield className="w-4 h-4" />
                  {status.isArmed ? 'DISARM GUARDIAN' : 'ARM GUARDIAN NOW'}
                </button>

                <button
                  onClick={onTriggerTrap}
                  className="flex-1 py-4 text-xs font-black tracking-widest uppercase border border-[#FF003C] text-[#FF003C] hover:bg-[#FF003C] hover:text-[#050505] transition-all cursor-pointer flex items-center justify-center gap-2"
                >
                  <Play className="w-4 h-4" />
                  TRIGGER SIMULATION (THE TRAP)
                </button>

                <button
                  onClick={handleEmergencyLock}
                  className="py-4 px-6 text-xs font-black tracking-widest uppercase border border-red-800 text-red-500 hover:bg-red-800 hover:text-white transition-all cursor-pointer flex items-center justify-center gap-2"
                >
                  <Skull className="w-4 h-4" />
                  EMERGENCY LOCK
                </button>
              </div>
            </div>

            {/* Quick Guide */}
            <div className="border border-[#333] bg-[#0a0a0a] p-6 space-y-4">
              <h2 className="text-xs font-bold text-[#FF003C] tracking-widest uppercase border-b border-[#222] pb-2">
                HOW THE TRAP OPERATES
              </h2>
              <ul className="text-xs text-[#8F8F8F] space-y-3 leading-relaxed">
                <li>
                  <strong className="text-white">1. Auto-Arming:</strong> When idle exceeds {config.idleThresholdSeconds}s, SnoopTrap arms automatically in the background.
                </li>
                <li>
                  <strong className="text-white">2. Breach:</strong> The moment someone moves the mouse or touches a key, the trap instantly triggers.
                </li>
                <li>
                  <strong className="text-white">3. Lockdown:</strong> Screen blurs (100vw/100vh), Alt+Tab/WinKey are blocked via Win32 Hook.
                </li>
                <li>
                  <strong className="text-white">4. Siren & Capture:</strong> Unmutes internal speaker to 100% volume, takes silent webcam photo, and sends OTP to Telegram.
                </li>
              </ul>
            </div>
          </div>
        ) : (
          /* Configuration Tab */
          <form onSubmit={handleSaveConfig} className="max-w-2xl border border-[#333] bg-[#0a0a0a] p-6 space-y-6">
            <div className="border-b border-[#222] pb-3 flex justify-between items-center">
              <h2 className="text-sm font-bold text-[#FF003C] tracking-widest uppercase">
                SYSTEM PREFERENCES (config.json)
              </h2>
              {saveSuccess && (
                <span className="text-xs text-[#00FF41] flex items-center gap-1">
                  <CheckCircle2 className="w-3.5 h-3.5" /> SAVED
                </span>
              )}
            </div>

            {/* Telegram Settings */}
            <div className="space-y-4">
              <h3 className="text-xs font-bold text-white tracking-widest uppercase">
                TELEGRAM BOT CONFIGURATION
              </h3>

              <div className="space-y-1">
                <label className="text-xs text-[#8F8F8F]">BOT API TOKEN</label>
                <input
                  type="text"
                  value={config.telegramBotToken}
                  onChange={(e) => setConfig({ ...config, telegramBotToken: e.target.value })}
                  placeholder="e.g. 7123456789:AAH..."
                  className="w-full bg-[#050505] border border-[#333] p-3 text-xs text-white focus:border-[#FF003C] focus:outline-none"
                />
              </div>

              <div className="space-y-1">
                <label className="text-xs text-[#8F8F8F]">OWNER CHAT ID</label>
                <input
                  type="text"
                  value={config.telegramChatId}
                  onChange={(e) => setConfig({ ...config, telegramChatId: e.target.value })}
                  placeholder="e.g. 123456789"
                  className="w-full bg-[#050505] border border-[#333] p-3 text-xs text-white focus:border-[#FF003C] focus:outline-none"
                />
              </div>
            </div>

            {/* Idle & Audio Settings */}
            <div className="space-y-4 pt-4 border-t border-[#222]">
              <h3 className="text-xs font-bold text-white tracking-widest uppercase">
                BEHAVIOR & HARDWARE OVERRIDE
              </h3>

              <div className="space-y-1">
                <label className="text-xs text-[#8F8F8F]">
                  IDLE THRESHOLD (SECONDS BEFORE AUTO-ARM)
                </label>
                <input
                  type="number"
                  min={10}
                  max={3600}
                  value={config.idleThresholdSeconds}
                  onChange={(e) =>
                    setConfig({ ...config, idleThresholdSeconds: parseInt(e.target.value) || 180 })
                  }
                  className="w-full bg-[#050505] border border-[#333] p-3 text-xs text-white focus:border-[#FF003C] focus:outline-none"
                />
              </div>

              <div className="space-y-1">
                <label className="text-xs text-[#8F8F8F]">
                  VIDEO RECORDING DURATION (SECONDS)
                </label>
                <input
                  type="number"
                  min={10}
                  max={300}
                  value={config.videoRecordDurationSeconds || 30}
                  onChange={(e) =>
                    setConfig({
                      ...config,
                      videoRecordDurationSeconds: parseInt(e.target.value) || 30,
                    })
                  }
                  className="w-full bg-[#050505] border border-[#333] p-3 text-xs text-white focus:border-[#FF003C] focus:outline-none"
                />
                <div className="text-[10px] text-[#8F8F8F]">
                  Current: {config.videoRecordDurationSeconds || 30}s surveillance evidence (customizable: 15s - 300s)
                </div>
              </div>

              <div className="space-y-1">
                <label className="text-xs text-[#8F8F8F]">
                  EMERGENCY MASTER PIN (BACKUP PASSCODE)
                </label>
                <input
                  type="text"
                  maxLength={6}
                  placeholder="e.g. 123456 (6-digit custom PIN)"
                  value={config.emergencyMasterPin || ''}
                  onChange={(e) =>
                    setConfig({
                      ...config,
                      emergencyMasterPin: e.target.value.replace(/[^0-9]/g, '').slice(0, 6),
                    })
                  }
                  className="w-full bg-[#050505] border border-[#333] p-3 text-xs text-white focus:border-[#FF003C] focus:outline-none"
                />
                <div className="text-[10px] text-[#8F8F8F]">
                  PIN 6 digit rahasia yang dapat Anda ubah sendiri untuk membuka kuncian darurat di layar laptop saat tidak ada HP/Telegram.
                </div>
              </div>

              <div className="space-y-1">
                <label className="text-xs text-[#8F8F8F]">
                  PUBLIC RELAY SERVER URL / VPS (OPTIONAL)
                </label>
                <input
                  type="text"
                  placeholder="e.g. http://103.123.45.67:8080 or https://stream.domain.com"
                  value={config.streamRelayServerUrl || ''}
                  onChange={(e) =>
                    setConfig({
                      ...config,
                      streamRelayServerUrl: e.target.value.trim(),
                    })
                  }
                  className="w-full bg-[#050505] border border-[#333] p-3 text-xs text-white focus:border-[#FF003C] focus:outline-none"
                />
                <div className="text-[10px] text-[#8F8F8F]">
                  Alamat IP/Domain VPS Anda. Jika diisi, link live stream Telegram dapat dibuka dari mana saja (termasuk kuota 4G HP di luar rumah). Jika dikosongkan, menggunakan IP Wi-Fi lokal.
                </div>
              </div>

              <div className="space-y-1">
                <label className="text-xs text-[#8F8F8F]">TARGET AUDIO PLAYBACK DEVICE</label>
                <select
                  value={config.targetAudioDeviceGuid}
                  onChange={(e) => setConfig({ ...config, targetAudioDeviceGuid: e.target.value })}
                  className="w-full bg-[#050505] border border-[#333] p-3 text-xs text-white focus:border-[#FF003C] focus:outline-none"
                >
                  <option value="default">Default Speaker / Device</option>
                  {audioDevices.map((d) => (
                    <option key={d.id} value={d.id}>
                      {d.name} {d.isDefault ? '(System Default)' : ''}
                    </option>
                  ))}
                </select>
              </div>

              <div className="flex items-center gap-3 pt-2">
                <input
                  type="checkbox"
                  id="autoArm"
                  checked={config.autoArm}
                  onChange={(e) => setConfig({ ...config, autoArm: e.target.checked })}
                  className="w-4 h-4 accent-[#FF003C] cursor-pointer"
                />
                <label htmlFor="autoArm" className="text-xs text-white cursor-pointer">
                  Enable Global Auto-Arming when idle
                </label>
              </div>
            </div>

            {/* Nuclear Option */}
            <div className="space-y-4 pt-4 border-t border-[#222]">
              <h3 className="text-xs font-bold text-white tracking-widest uppercase">
                THE NUCLEAR OPTION (FAIL-SAFE)
              </h3>

              <div className="space-y-1">
                <label className="text-xs text-[#8F8F8F]">ACTION ON FORCED BYPASS / LOST FOCUS</label>
                <select
                  value={config.nuclearAction}
                  onChange={(e) => setConfig({ ...config, nuclearAction: e.target.value })}
                  className="w-full bg-[#050505] border border-[#333] p-3 text-xs text-white focus:border-[#FF003C] focus:outline-none"
                >
                  <option value="lock">Lock Windows (user32.dll LockWorkStation)</option>
                  <option value="taskkill">Terminate Sensitive Process (taskkill /F)</option>
                </select>
              </div>

              {config.nuclearAction === 'taskkill' && (
                <div className="space-y-1">
                  <label className="text-xs text-[#8F8F8F]">PROCESS NAME TO KILL</label>
                  <input
                    type="text"
                    value={config.targetKillProcess}
                    onChange={(e) => setConfig({ ...config, targetKillProcess: e.target.value })}
                    placeholder="e.g. WhatsApp.exe"
                    className="w-full bg-[#050505] border border-[#333] p-3 text-xs text-white focus:border-[#FF003C] focus:outline-none"
                  />
                </div>
              )}
            </div>

            <button
              type="submit"
              disabled={isSaving}
              className="w-full py-4 text-xs font-bold tracking-widest uppercase bg-[#FF003C] text-[#050505] hover:bg-[#FF003C]/90 transition-all cursor-pointer"
            >
              {isSaving ? 'SAVING...' : 'SAVE SETTINGS'}
            </button>
          </form>
        )}

        {/* Live Terminal Telemetry Box */}
        <section className="border border-[#333] bg-[#050505] p-4 text-xs">
          <div className="flex items-center gap-2 text-[#00FF41] border-b border-[#222] pb-2 mb-2 font-bold">
            <Terminal className="w-3.5 h-3.5" />
            <span>ACTIVITY LOGS // SYSTEM TRACE</span>
          </div>

          <div className="h-32 overflow-y-auto space-y-1 text-[#00FF41]/80 font-mono text-[11px] scrollbar-thin">
            {logs.map((log, idx) => (
              <div key={idx} className="leading-tight">
                {log}
              </div>
            ))}
          </div>
        </section>
      </main>
    </div>
  );
};
