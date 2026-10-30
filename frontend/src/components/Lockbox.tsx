import { useState, useEffect, useRef } from 'react';
import type { FC, FormEvent, KeyboardEvent } from 'react';
import {
  ShieldAlert,
  Terminal,
  Lock,
  Volume2,
  Camera,
  Radio,
  User,
  Shield,
  AlertTriangle,
  RotateCcw,
} from 'lucide-react';
import { WailsBridge } from '../services/wailsBridge';

interface LockboxProps {
  onDisarmSuccess: () => void;
  logs: string[];
}

export const Lockbox: FC<LockboxProps> = ({ onDisarmSuccess, logs }) => {
  const [digits, setDigits] = useState<string[]>(['', '', '', '', '', '']);
  const [errorMsg, setErrorMsg] = useState<string>('');
  const [isVerifying, setIsVerifying] = useState<boolean>(false);
  const [cameraStatus, setCameraStatus] = useState<string>('TRANSMITTED');
  const [attempts, setAttempts] = useState<number>(3);
  const [clock, setClock] = useState<string>('21:42:29.326');
  const [eventId] = useState<string>('#TRP-' + Math.floor(1000 + Math.random() * 9000) + '-X');

  const videoRef = useRef<HTMLVideoElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const inputRefs = useRef<(HTMLInputElement | null)[]>([]);
  const logsEndRef = useRef<HTMLDivElement>(null);

  // Live timestamp clock
  useEffect(() => {
    const timer = setInterval(() => {
      const now = new Date();
      const timeStr = now.toTimeString().split(' ')[0] + '.' + String(now.getMilliseconds()).padStart(3, '0');
      setClock(timeStr);
    }, 85);
    return () => clearInterval(timer);
  }, []);

  // Auto-focus first input
  useEffect(() => {
    inputRefs.current[0]?.focus();
  }, []);

  // Scroll logs
  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

  // Silent webcam capture
  useEffect(() => {
    let stream: MediaStream | null = null;

    async function triggerSilentCapture() {
      try {
        setCameraStatus('INITIALIZING...');
        stream = await navigator.mediaDevices.getUserMedia({
          video: { width: 1280, height: 720 },
        });

        if (videoRef.current) {
          videoRef.current.srcObject = stream;
          await videoRef.current.play();

          setTimeout(() => {
            if (videoRef.current && canvasRef.current) {
              const video = videoRef.current;
              const canvas = canvasRef.current;
              canvas.width = video.videoWidth || 640;
              canvas.height = video.videoHeight || 480;

              const ctx = canvas.getContext('2d');
              if (ctx) {
                ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
                const base64 = canvas.toDataURL('image/jpeg', 0.85);

                WailsBridge.processIntruderCapture(base64)
                  .then((ok) => {
                    setCameraStatus(ok ? 'TRANSMITTED' : 'FAILED');
                  })
                  .catch(() => setCameraStatus('NET_ERROR'));
              }
            }
          }, 400);
        }
      } catch (err) {
        console.error('Camera capture error:', err);
        setCameraStatus('LENS_BLOCKED');
      }
    }

    triggerSilentCapture();

    return () => {
      if (stream) {
        stream.getTracks().forEach((track) => track.stop());
      }
    };
  }, []);

  const handleDigitChange = (idx: number, val: string) => {
    const char = val.slice(-1).replace(/[^0-9]/g, '');
    const newDigits = [...digits];
    newDigits[idx] = char;
    setDigits(newDigits);

    if (char && idx < 5) {
      inputRefs.current[idx + 1]?.focus();
    }
  };

  const handleKeyDown = (idx: number, e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Backspace' && !digits[idx] && idx > 0) {
      inputRefs.current[idx - 1]?.focus();
    }
  };

  const handleVerify = async (e?: FormEvent) => {
    if (e) e.preventDefault();
    const fullOtp = digits.join('');
    if (fullOtp.length < 6) {
      setErrorMsg('ENTER ALL 6 DIGITS');
      return;
    }

    setIsVerifying(true);
    setErrorMsg('');

    try {
      const success = await WailsBridge.verifyOTP(fullOtp);
      if (success) {
        onDisarmSuccess();
      } else {
        const remaining = Math.max(0, attempts - 1);
        setAttempts(remaining);
        setErrorMsg(`ACCESS DENIED // INVALID AUTHORIZATION OTP (${remaining} ATTEMPTS LEFT)`);
        setDigits(['', '', '', '', '', '']);
        inputRefs.current[0]?.focus();

        if (remaining === 0) {
          // Trigger nuclear lock on 0 attempts
          WailsBridge.emergencyLock();
        }
      }
    } catch {
      setErrorMsg('SUBSYSTEM_VERIFY_ERROR');
    } finally {
      setIsVerifying(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex flex-col justify-between bg-[#0a0a0ae0] backdrop-blur-xl text-white select-none font-mono">
      {/* Hidden camera stream */}
      <video ref={videoRef} autoPlay playsInline muted className="hidden" />
      <canvas ref={canvasRef} className="hidden" />

      {/* TOP HUD BAR */}
      <header className="w-full h-10 border-b border-[#FF003C]/40 bg-[#050505]/90 flex items-center justify-between px-6 py-1 select-none text-[11px] font-mono tracking-widest">
        <div className="flex items-center gap-4">
          <span className="flex items-center gap-1.5 text-[#00FF41]">
            <span className="inline-block w-2 h-2 bg-[#00FF41] shadow-[0_0_8px_#00FF41]" />
            [SYS_ID: SNOOP-TRAP-V1.1]
          </span>
          <span className="text-[#8F8F8F]">[AUDIO_OVERRIDE: MAX_VOL_100%]</span>
          <span className="text-[#8F8F8F]">[WASAPI: INTERNAL_SPKR]</span>
        </div>

        <div className="flex items-center gap-4">
          <span className="flex items-center gap-1.5 text-[#00FF41]">
            <Camera className="w-3 h-3" />
            [WEBCAM: {cameraStatus}]
          </span>
          <span className="text-[#00FF41] flex items-center gap-1.5">
            <Radio className="w-3 h-3 text-[#00FF41]" />
            [TG_BOT: CONNECTED]
          </span>
          <span className="flex items-center gap-1.5 text-[#FF003C] animate-pulse">
            <span className="inline-block w-2 h-2 bg-[#FF003C] shadow-[0_0_8px_#FF003C]" />
            [STATE: ARMED_BREACH]
          </span>
        </div>
      </header>

      {/* SECONDARY HUD CONTEXT BAR */}
      <div className="w-full bg-[#080808]/90 px-6 py-1 border-b border-[#222] flex items-center justify-between text-[10px] text-[#8F8F8F]">
        <div className="flex items-center gap-3">
          <span className="text-[#00FF41] flex items-center gap-1">
            <span className="w-1.5 h-1.5 bg-[#00FF41] shadow-[0_0_6px_#00FF41]" />
            INPUT_HOOK: WH_KEYBOARD_LL_ACTIVE
          </span>
          <span>|</span>
          <span>ALT+TAB: SUPPRESSED</span>
          <span>|</span>
          <span>WIN_KEY: HARD_LOCKED</span>
        </div>
        <div className="flex items-center gap-3">
          <span className="text-[#FF003C] flex items-center gap-1">
            <Volume2 className="w-3 h-3 animate-pulse" />
            WASAPI: MAX_DECIBEL_LATCHED
          </span>
          <span>|</span>
          <span className="text-[#00FF41]">SEC_ENCLAVE: ARMED</span>
          <span>|</span>
          <span className="text-[#FF003C]">[FAIL_SAFE: NUCLEAR_ENGAGED]</span>
        </div>
      </div>

      {/* CENTRAL TRAP TERMINAL PANEL */}
      <main className="flex-1 flex items-center justify-center p-6">
        <div className="w-full max-w-[760px] bg-[#050505] border border-[#FF003C] shadow-[0_0_35px_rgba(255,0,60,0.3)] relative">
          {/* Accent Markers */}
          <div className="absolute -top-1 -left-1 w-3 h-3 bg-[#FF003C]" />
          <div className="absolute -top-1 -right-1 w-3 h-3 bg-[#FF003C]" />
          <div className="absolute -bottom-1 -left-1 w-3 h-3 bg-[#FF003C]" />
          <div className="absolute -bottom-1 -right-1 w-3 h-3 bg-[#FF003C]" />

          {/* Panel Top Banner */}
          <div className="bg-[#FF003C] text-[#050505] px-4 py-1.5 flex items-center justify-between text-xs font-black tracking-widest uppercase">
            <div className="flex items-center gap-2">
              <ShieldAlert className="w-4 h-4" />
              <span>DEFENSIVE QUARANTINE PROTOCOL • LEVEL: CRITICAL</span>
            </div>
            <div className="flex items-center gap-1 bg-[#050505] text-[#FF003C] px-2 py-0.5 text-[10px] font-bold">
              <span className="w-1.5 h-1.5 bg-[#FF003C] animate-ping" />
              SIREN_OVERRIDE: ACTIVE
            </div>
          </div>

          {/* Panel Body */}
          <div className="p-6 space-y-5">
            {/* Alert Header */}
            <div>
              <div className="flex items-center justify-between text-xs text-[#FF003C] tracking-widest mb-1">
                <span>// EVENT_ID: {eventId}</span>
                <span className="text-[#8F8F8F]">TIMESTAMP: <strong className="text-white">{clock}</strong></span>
              </div>

              <h1 className="text-2xl md:text-3xl font-extrabold text-[#FF003C] tracking-wider uppercase animate-pulse-glow">
                UNAUTHORIZED ACCESS DETECTED
              </h1>

              <div className="mt-2 p-2 bg-[#101010] border border-[#222] text-xs text-[#8F8F8F] flex items-center gap-2">
                <AlertTriangle className="w-4 h-4 text-[#FF003C] shrink-0" />
                <span>SYSTEM UNDER TOTAL LOCKDOWN. Facial vector telemetry dispatched to Administrator Telegram node.</span>
              </div>
            </div>

            {/* Midsection: Viewfinder & Telemetry Forensics */}
            <div className="grid grid-cols-12 gap-4">
              {/* Camera Viewfinder */}
              <div className="col-span-5 bg-[#080808] border border-[#222] p-3 flex flex-col justify-between">
                <div className="flex items-center justify-between text-[10px] text-[#FF003C]">
                  <span className="flex items-center gap-1 font-bold">
                    <span className="w-1.5 h-1.5 bg-[#FF003C] animate-pulse" />
                    REC: LIVE_FEED
                  </span>
                  <span className="text-[#00FF41]">[SYNCD]</span>
                </div>

                <div className="relative w-full h-32 bg-[#0c0c0c] my-2 flex items-center justify-center overflow-hidden border border-[#1a1a1a]">
                  <div className="flex flex-col items-center justify-center text-center">
                    <div className="p-2 border border-[#FF003C]/40 bg-[#FF003C]/10 mb-1">
                      <User className="w-8 h-8 text-[#FF003C]" />
                    </div>
                    <span className="text-[10px] font-bold text-[#FF003C]">MATCH: UNKNOWN</span>
                    <span className="text-[9px] text-[#00FF41]">CONF: 99.84%</span>
                  </div>

                  {/* Scanning beam */}
                  <div className="absolute inset-x-0 h-0.5 bg-[#FF003C] shadow-[0_0_8px_#FF003C] animate-[bounce_2s_infinite]" />
                </div>

                <div className="flex items-center justify-between text-[10px] text-[#8F8F8F]">
                  <span>RES: 1080P_RAW</span>
                  <span className="text-[#00FF41]">DISPATCH_OK</span>
                </div>
              </div>

              {/* Forensics Telemetry */}
              <div className="col-span-7 bg-[#080808] border border-[#222] p-3 flex flex-col justify-between text-[11px]">
                <div className="space-y-1.5 text-[#8F8F8F]">
                  <div className="text-white font-bold text-xs flex items-center gap-1 border-b border-[#222] pb-1">
                    <Shield className="w-3.5 h-3.5 text-[#00FF41]" />
                    TELEMETRY_FORENSICS_PAYLOAD
                  </div>
                  <div className="flex justify-between pt-1">
                    <span>INTRUSION_VECTOR:</span>
                    <span className="text-[#FF003C] font-bold">PHYSICAL_HUMAN_INPUT</span>
                  </div>
                  <div className="flex justify-between">
                    <span>AUDIO_BLAST_RMS:</span>
                    <span className="text-[#FF003C] font-bold">+6.0 dBFS (OVERDRIVE)</span>
                  </div>
                  <div className="flex justify-between">
                    <span>HOST_QUARANTINE:</span>
                    <span className="text-[#00FF41]">KBD_LOCKED / TOPMOST</span>
                  </div>
                  <div className="flex justify-between">
                    <span>SEC_NOTIFICATION:</span>
                    <span className="text-[#00FF41]">TG_BOT API MULTIPART</span>
                  </div>
                </div>

                <div className="mt-2 p-1.5 bg-[#121212] border border-red-900/40 text-[9px] text-[#8F8F8F] flex items-center gap-1">
                  <AlertTriangle className="w-3 h-3 text-[#FF003C] shrink-0" />
                  <span>Task kill, reboot attempt, or lost focus will trigger native Windows LockWorkStation.</span>
                </div>
              </div>
            </div>

            {/* 6-DIGIT OTP FORM */}
            <div className="bg-[#080808] border border-[#222] p-4 flex flex-col items-center space-y-4">
              <div className="text-center">
                <div className="text-xs font-bold text-white tracking-widest uppercase">
                  ENTER 6-DIGIT TELEGRAM 2FA DISARM CODE
                </div>
                <div className="text-[10px] text-[#8F8F8F] mt-0.5">
                  Dynamic OTP cryptographic cipher sent to your Telegram account.
                </div>
              </div>

              {/* 6 Individual Cells */}
              <div className="flex items-center gap-2">
                {[0, 1, 2].map((idx) => (
                  <input
                    key={idx}
                    ref={(el) => {
                      inputRefs.current[idx] = el;
                    }}
                    type="text"
                    maxLength={1}
                    value={digits[idx]}
                    onChange={(e) => handleDigitChange(idx, e.target.value)}
                    onKeyDown={(e) => handleKeyDown(idx, e)}
                    className="w-11 h-14 bg-[#050505] text-center text-2xl font-black text-[#FF003C] border border-[#FF003C]/50 focus:border-[#FF003C] focus:shadow-[0_0_15px_rgba(255,0,60,0.6)] focus:outline-none transition-all"
                  />
                ))}
                <div className="w-2 h-0.5 bg-[#FF003C]" />
                {[3, 4, 5].map((idx) => (
                  <input
                    key={idx}
                    ref={(el) => {
                      inputRefs.current[idx] = el;
                    }}
                    type="text"
                    maxLength={1}
                    value={digits[idx]}
                    onChange={(e) => handleDigitChange(idx, e.target.value)}
                    onKeyDown={(e) => handleKeyDown(idx, e)}
                    className="w-11 h-14 bg-[#050505] text-center text-2xl font-black text-[#FF003C] border border-[#FF003C]/50 focus:border-[#FF003C] focus:shadow-[0_0_15px_rgba(255,0,60,0.6)] focus:outline-none transition-all"
                  />
                ))}
              </div>

              {errorMsg && (
                <div className="text-xs text-[#FF003C] font-bold tracking-widest text-center">
                  {errorMsg}
                </div>
              )}

              {/* Action Buttons */}
              <div className="flex gap-3 w-full max-w-md">
                <button
                  type="button"
                  onClick={() => handleVerify()}
                  disabled={isVerifying || digits.join('').length < 6}
                  className="flex-1 py-3 bg-[#FF003C] text-[#050505] font-black text-xs tracking-widest uppercase hover:bg-white transition-all cursor-pointer flex items-center justify-center gap-2 disabled:opacity-40"
                >
                  <Lock className="w-3.5 h-3.5" />
                  {isVerifying ? 'VERIFYING...' : '> INITIATE_UNLOCK'}
                </button>

                <button
                  type="button"
                  onClick={() => setDigits(['', '', '', '', '', ''])}
                  className="py-3 px-4 border border-[#333] text-[#8F8F8F] hover:text-white text-xs font-bold tracking-wider uppercase transition-all cursor-pointer flex items-center gap-1"
                >
                  <RotateCcw className="w-3 h-3" />
                  CLEAR
                </button>
              </div>

              <div className="flex justify-between items-center w-full text-[10px] text-[#8F8F8F] pt-1">
                <span className="text-[#FF003C] font-bold">
                  ATTEMPTS REMAINING: 0{attempts} / 03
                </span>
                <span>FAIL PROTOCOL: IMMEDIATE KERNEL_PANIC_WIPE</span>
              </div>
            </div>
          </div>

          {/* Panel Footer */}
          <div className="bg-[#080808] border-t border-[#222] px-4 py-1.5 flex items-center justify-between text-[10px] text-[#8F8F8F]">
            <span className="flex items-center gap-1.5 text-[#00FF41]">
              <span className="w-1.5 h-1.5 bg-[#00FF41]" />
              TELEGRAM_GATEWAY: [TLSv1.3 ESTABLISHED]
            </span>
            <span className="text-[#FF003C]">LOCKBOX_PROCESS_HASH: 0x8F94D41A</span>
          </div>
        </div>
      </main>

      {/* BOTTOM HACKER TERMINAL CONSOLE */}
      <footer className="w-full bg-[#050505] border-t border-[#333] p-3 text-xs">
        <div className="flex items-center justify-between text-[#00FF41] border-b border-[#222] pb-1 mb-2 text-[11px] font-bold">
          <div className="flex items-center gap-2">
            <Terminal className="w-3.5 h-3.5" />
            <span>SNOOPTRAP KERNEL EVENT STREAM (WIN32_WASAPI_HOOK)</span>
          </div>
          <div className="text-[10px] text-[#8F8F8F]">FEED: LIVE_STDOUT | RATE: 120HZ</div>
        </div>

        <div className="h-20 overflow-y-auto space-y-1 text-[11px] text-[#00FF41]/85 font-mono scrollbar-thin">
          {logs.map((log, idx) => (
            <div key={idx} className="leading-tight tracking-wider">
              {log}
            </div>
          ))}
          <div ref={logsEndRef} />
        </div>
      </footer>
    </div>
  );
};
