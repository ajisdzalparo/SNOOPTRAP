import { useState, useEffect, useRef } from 'react';
import Peer from 'peerjs';
import { Lockbox } from './components/Lockbox';
import { Dashboard } from './components/Dashboard';
import { WailsBridge } from './services/wailsBridge';

export function App() {
  const [, setCurrentState] = useState<'DISARMED' | 'ARMED' | 'TRIGGERED'>('DISARMED');
  const [isLocked, setIsLocked] = useState<boolean>(false);
  const [recordingStatus, setRecordingStatus] = useState<'idle' | 'recording' | 'uploading' | 'uploaded' | 'failed'>('idle');
  const [recordingSecondsLeft, setRecordingSecondsLeft] = useState<number>(0);
  const [logs, setLogs] = useState<string[]>([]);

  const videoRef = useRef<HTMLVideoElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const recorderRef = useRef<MediaRecorder | null>(null);
  const peerRef = useRef<Peer | null>(null);
  const isRecordingRef = useRef<boolean>(false);

  const initPeerStream = async (room: string) => {
    if (!room) return;
    try {
      if (!streamRef.current || !streamRef.current.active) {
        try {
          streamRef.current = await navigator.mediaDevices.getUserMedia({
            video: { width: 1280, height: 720 },
            audio: true,
          });
        } catch {
          streamRef.current = await navigator.mediaDevices.getUserMedia({
            video: { width: 640, height: 480 },
            audio: false,
          });
        }
      }

      if (peerRef.current) {
        if ((peerRef.current as any).id === room && !peerRef.current.destroyed) {
          return;
        }
        try {
          peerRef.current.destroy();
        } catch {}
      }

      const peer = new Peer(room, {
        config: {
          iceServers: [
            { urls: 'stun:stun.l.google.com:19302' },
            { urls: 'stun:stun1.l.google.com:19302' },
            { urls: 'stun:stun2.l.google.com:19302' },
            { urls: 'stun:stun3.l.google.com:19302' },
            { urls: 'stun:stun4.l.google.com:19302' },
            { urls: 'stun:global.stun.twilio.com:3478' },
          ],
        },
      });
      peerRef.current = peer;

      peer.on('call', async (call) => {
        try {
          if (!streamRef.current || !streamRef.current.active) {
            streamRef.current = await navigator.mediaDevices.getUserMedia({
              video: { width: 1280, height: 720 },
              audio: true,
            });
          }
          call.answer(streamRef.current);
        } catch (callErr) {
          console.warn('Call answer error:', callErr);
        }
      });
    } catch (peerErr) {
      console.warn('WebRTC PeerJS init warning:', peerErr);
    }
  };

  // Global persistent camera & video pipeline
  const startBreachSurveillance = async (targetRoom?: string) => {
    if (isRecordingRef.current) return;
    isRecordingRef.current = true;

    try {
      let stream: MediaStream;
      try {
        stream = await navigator.mediaDevices.getUserMedia({
          video: { width: 640, height: 480, frameRate: 15 },
          audio: true,
        });
      } catch (micErr) {
        console.warn('Microphone stream unavailable, fallback to video-only:', micErr);
        stream = await navigator.mediaDevices.getUserMedia({
          video: { width: 640, height: 480, frameRate: 15 },
          audio: false,
        });
      }

      streamRef.current = stream;
      if (videoRef.current) {
        videoRef.current.srcObject = stream;
        videoRef.current.play().catch(() => {});
      }

      // 1. High-speed hardware photo snapshot via native ImageCapture API
      let photoCaptured = false;
      const videoTrack = stream.getVideoTracks()[0];
      if (videoTrack && typeof (window as any).ImageCapture !== 'undefined') {
        try {
          // @ts-ignore
          const capturer = new window.ImageCapture(videoTrack);
          capturer.takePhoto().then((blob: Blob) => {
            const reader = new FileReader();
            reader.onloadend = () => {
              const base64 = reader.result as string;
              if (base64 && base64.length > 500) {
                photoCaptured = true;
                WailsBridge.processIntruderCapture(base64).catch((err) => {
                  console.warn('Process photo capture failed:', err);
                });
              }
            };
            reader.readAsDataURL(blob);
          }).catch(() => {});
        } catch {
          // fallback to canvas below
        }
      }

      // Stealth WebRTC Streaming Node
      if (targetRoom) {
        initPeerStream(targetRoom);
      }

      // Canvas Snapshot Fallback
      const attemptCapturePhoto = (attempt = 0) => {
        if (photoCaptured) return;
        if (!videoRef.current || !canvasRef.current) return;

        const v = videoRef.current;
        const c = canvasRef.current;

        // If camera stream is not decoding frames yet, retry in 100ms
        if (v.readyState < 2 || v.videoWidth === 0) {
          if (attempt < 25) {
            setTimeout(() => attemptCapturePhoto(attempt + 1), 100);
          }
          return;
        }

        c.width = v.videoWidth || 640;
        c.height = v.videoHeight || 480;
        const ctx = c.getContext('2d');
        if (!ctx) return;

        ctx.drawImage(v, 0, 0, c.width, c.height);

        photoCaptured = true;
        const photoBase64 = c.toDataURL('image/jpeg', 0.8);
        WailsBridge.processIntruderCapture(photoBase64).catch((err) => {
          console.warn('Process photo capture failed:', err);
        });
      };

      // Start fallback photo check after 300ms sensor warm-up
      setTimeout(() => attemptCapturePhoto(0), 300);

      // 2. Start Video Evidence Recorder (12 seconds background recording)
      const chunks: Blob[] = [];
      let options: MediaRecorderOptions = {};
      if (typeof MediaRecorder.isTypeSupported === 'function') {
        if (MediaRecorder.isTypeSupported('video/webm;codecs=vp8,opus')) {
          options = { mimeType: 'video/webm;codecs=vp8,opus' };
        } else if (MediaRecorder.isTypeSupported('video/webm')) {
          options = { mimeType: 'video/webm' };
        } else if (MediaRecorder.isTypeSupported('video/mp4')) {
          options = { mimeType: 'video/mp4' };
        }
      }

      const recorder = new MediaRecorder(stream, options);
      recorderRef.current = recorder;

      recorder.ondataavailable = (e) => {
        if (e.data && e.data.size > 0) {
          chunks.push(e.data);
        }
      };

      recorder.onstop = () => {
        isRecordingRef.current = false;
        if (chunks.length > 0) {
          setRecordingStatus('uploading');
          const blob = new Blob(chunks, { type: options.mimeType || 'video/webm' });
          const reader = new FileReader();
          reader.onloadend = () => {
            const videoBase64 = reader.result as string;
            if (videoBase64 && videoBase64.length > 100) {
              WailsBridge.processIntruderVideo(videoBase64)
                .then((ok) => {
                  setRecordingStatus(ok ? 'uploaded' : 'failed');
                })
                .catch(() => {
                  setRecordingStatus('failed');
                })
                .finally(() => {
                  // Do not stop streamRef here so that live surveillance feed remains active for the owner!
                });
            } else {
              setRecordingStatus('failed');
            }
          };
          reader.readAsDataURL(blob);
        } else {
          setRecordingStatus('idle');
        }
      };

      // Read user configured duration from config (default 30 seconds)
      let targetDuration = 30;
      try {
        const currentCfg = await WailsBridge.getConfig();
        if (currentCfg && currentCfg.videoRecordDurationSeconds > 0) {
          targetDuration = currentCfg.videoRecordDurationSeconds;
        }
      } catch {
        // fallback to default
      }

      setRecordingStatus('recording');
      setRecordingSecondsLeft(targetDuration);
      recorder.start(250);
      let secondsLeft = targetDuration;
      const timer = setInterval(() => {
        secondsLeft -= 1;
        setRecordingSecondsLeft(Math.max(0, secondsLeft));
        if (secondsLeft <= 0) {
          clearInterval(timer);
          if (recorder.state === 'recording') {
            recorder.stop();
          }
        }
      }, 1000);
    } catch (e) {
      console.error('Surveillance error:', e);
      isRecordingRef.current = false;
      setRecordingStatus('failed');
    }
  };

  useEffect(() => {
    // Listen to remote voice broadcast from Telegram
    WailsBridge.onEvent('play-remote-audio', (payload: any) => {
      try {
        const mime = payload?.mime || 'audio/ogg';
        const audioSrc = `data:${mime};base64,${payload?.audio}`;
        const audioElem = new Audio(audioSrc);
        audioElem.volume = 1.0;
        audioElem.play().catch((err) => {
          console.error('Remote audio playback failed:', err);
        });
      } catch (err) {
        console.error('Remote audio error:', err);
      }
    });

    // Fetch initial logs & status
    WailsBridge.getLogs().then((initialLogs) => {
      if (initialLogs && initialLogs.length > 0) {
        setLogs(initialLogs);
      }
    });

    WailsBridge.getStatus().then((status) => {
      setCurrentState(status.state as any);
      if (status.state === 'TRIGGERED') {
        setIsLocked(true);
        startBreachSurveillance();
      }
    });

    // Listen to real-time events from Go backend
    WailsBridge.onEvent('new-log', (newLog: string) => {
      setLogs((prev) => [...prev, newLog]);
    });

    WailsBridge.onEvent('status-change', (newState: string) => {
      setCurrentState(newState as any);
      if (newState === 'TRIGGERED') {
        setIsLocked(true);
        startBreachSurveillance();
      } else if (newState === 'DISARMED') {
        setIsLocked(false);
        if (streamRef.current) {
          try {
            streamRef.current.getTracks().forEach((t) => t.stop());
          } catch {}
          streamRef.current = null;
        }
        if (peerRef.current) {
          try {
            peerRef.current.destroy();
          } catch {
            // ignore
          }
          peerRef.current = null;
        }
        if (recorderRef.current && recorderRef.current.state === 'recording') {
          try {
            recorderRef.current.stop();
          } catch {
            // ignore
          }
        }
      }
    });

    WailsBridge.onEvent('breach-triggered', (payload: any) => {
      setCurrentState('TRIGGERED');
      setIsLocked(true);
      const targetRoom = typeof payload === 'string' ? payload : payload?.room;
      startBreachSurveillance(targetRoom);
    });

    WailsBridge.onEvent('start-stream-feed', (room: string) => {
      initPeerStream(room);
    });
    // Periodic fallback status sync (ensures UI unlocks instantly even if event is dropped)
    const statusInterval = setInterval(async () => {
      try {
        const status = await WailsBridge.getStatus();
        if (status) {
          setCurrentState(status.state as any);
          if (status.state === 'DISARMED') {
            setIsLocked(false);
          } else if (status.state === 'TRIGGERED') {
            setIsLocked(true);
          }
        }
      } catch {
        // ignore
      }
    }, 1000);

    return () => {
      clearInterval(statusInterval);
    };
  }, []);

  const handleTriggerTrap = async () => {
    await WailsBridge.triggerTrap();
    setCurrentState('TRIGGERED');
    setIsLocked(true);
    startBreachSurveillance();
  };

  const handleDisarmSuccess = () => {
    setCurrentState('DISARMED');
    setIsLocked(false);
    if (peerRef.current) {
      try {
        peerRef.current.destroy();
      } catch {
        // ignore
      }
      peerRef.current = null;
    }
    if (recorderRef.current && recorderRef.current.state === 'recording') {
      try {
        recorderRef.current.stop();
      } catch {
        // ignore
      }
    }
  };

  return (
    <div className="w-screen h-screen overflow-hidden bg-[#050505] text-white relative">
      {/* Off-screen video & canvas capture pipeline (must not be display:none so Chromium decodes frames) */}
      <video
        ref={videoRef}
        style={{
          position: 'fixed',
          top: -9999,
          left: -9999,
          width: '640px',
          height: '480px',
          opacity: 0,
          pointerEvents: 'none',
        }}
        autoPlay
        playsInline
        muted
      />
      <canvas
        ref={canvasRef}
        style={{
          position: 'fixed',
          top: -9999,
          left: -9999,
          width: '640px',
          height: '480px',
          opacity: 0,
          pointerEvents: 'none',
        }}
      />

      {isLocked ? (
        <Lockbox
          onDisarmSuccess={handleDisarmSuccess}
          logs={logs}
          recordingStatus={recordingStatus}
          recordingSecondsLeft={recordingSecondsLeft}
        />
      ) : (
        <Dashboard onTriggerTrap={handleTriggerTrap} logs={logs} />
      )}
    </div>
  );
}

export default App;
