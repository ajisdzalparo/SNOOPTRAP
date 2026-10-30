import { useState, useEffect, useRef } from 'react';
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
  const micRecorderRef = useRef<MediaRecorder | null>(null);
  const streamWsRef = useRef<WebSocket | null>(null);
  const frameIntervalRef = useRef<any>(null);
  const isRecordingRef = useRef<boolean>(false);

  const stopAllStreaming = () => {
    if (frameIntervalRef.current) {
      clearInterval(frameIntervalRef.current);
      frameIntervalRef.current = null;
    }
    if (micRecorderRef.current && micRecorderRef.current.state === 'recording') {
      try {
        micRecorderRef.current.stop();
      } catch {}
      micRecorderRef.current = null;
    }
    if (streamWsRef.current) {
      try {
        streamWsRef.current.close();
      } catch {}
      streamWsRef.current = null;
    }
    if (recorderRef.current && recorderRef.current.state === 'recording') {
      try {
        recorderRef.current.stop();
      } catch {}
      recorderRef.current = null;
    }
    if (streamRef.current) {
      try {
        streamRef.current.getTracks().forEach((t) => t.stop());
      } catch {}
      streamRef.current = null;
    }
    isRecordingRef.current = false;
  };

  const startWebSocketStream = async (targetRoom: string, stream: MediaStream) => {
    try {
      if (streamWsRef.current) {
        streamWsRef.current.close();
        streamWsRef.current = null;
      }
      if (frameIntervalRef.current) {
        clearInterval(frameIntervalRef.current);
        frameIntervalRef.current = null;
      }
      if (micRecorderRef.current && micRecorderRef.current.state === 'recording') {
        try {
          micRecorderRef.current.stop();
        } catch {}
        micRecorderRef.current = null;
      }

      const wsUrl = await WailsBridge.getStreamWebSocketURL();
      if (!wsUrl) return;

      const ws = new WebSocket(wsUrl);
      streamWsRef.current = ws;

      ws.onopen = () => {
        // 1. Broadcast video frames to WebSocket relay at ~15 FPS
        frameIntervalRef.current = setInterval(() => {
          if (ws.readyState !== WebSocket.OPEN) return;
          const v = videoRef.current;
          const c = canvasRef.current;
          if (!v || !c || v.readyState < 2) return;

          c.width = 640;
          c.height = 480;
          const ctx = c.getContext('2d');
          if (!ctx) return;
          ctx.drawImage(v, 0, 0, 640, 480);
          const frameData = c.toDataURL('image/jpeg', 0.5);
          ws.send(JSON.stringify({ type: 'frame', data: frameData, room: targetRoom }));
        }, 66);

        // 2. Broadcast laptop microphone audio chunks every 400ms
        const audioTracks = stream.getAudioTracks();
        if (audioTracks.length > 0) {
          try {
            const micStream = new MediaStream(audioTracks);
            let mime = 'audio/webm';
            if (typeof MediaRecorder !== 'undefined' && MediaRecorder.isTypeSupported && MediaRecorder.isTypeSupported('audio/mp4')) {
              mime = 'audio/mp4';
            }
            const micRec = new MediaRecorder(micStream, { mimeType: mime, audioBitsPerSecond: 64000 });
            micRecorderRef.current = micRec;
            micRec.ondataavailable = (ev) => {
              if (ev.data && ev.data.size > 0 && ws.readyState === WebSocket.OPEN) {
                const reader = new FileReader();
                reader.onloadend = () => {
                  const b64 = (reader.result as string)?.split(',')[1];
                  if (b64) {
                    ws.send(JSON.stringify({ type: 'audio_out', data: b64, mime }));
                  }
                };
                reader.readAsDataURL(ev.data);
              }
            };
            micRec.start(400);
          } catch (micErr) {
            console.warn('Microphone audio streaming error:', micErr);
          }
        }
      };

      // 3. Receive 2-way intercom voice note from phone viewer holding "TAHAN BICARA"
      ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data);
          if (msg.type === 'intercom_speak' && msg.data) {
            WailsBridge.playRemoteVoice(msg.data, msg.mime || 'audio/webm');
          }
        } catch {}
      };

      ws.onerror = (e) => {
        console.warn('Live stream WebSocket warning:', e);
      };
    } catch (err) {
      console.warn('Failed to start WebSocket streaming:', err);
    }
  };

  // Global persistent camera & video pipeline
  const startBreachSurveillance = async (targetRoom?: string) => {
    if (isRecordingRef.current) return;
    isRecordingRef.current = true;

    try {
      let stream: MediaStream;
      if (streamRef.current && streamRef.current.active) {
        stream = streamRef.current;
      } else {
        try {
          stream = await navigator.mediaDevices.getUserMedia({
            video: { width: 640, height: 480, frameRate: 20 },
            audio: true,
          });
        } catch (micErr) {
          console.warn('Microphone stream unavailable, fallback to video-only:', micErr);
          stream = await navigator.mediaDevices.getUserMedia({
            video: { width: 640, height: 480, frameRate: 20 },
            audio: false,
          });
        }
        streamRef.current = stream;
      }

      if (videoRef.current) {
        videoRef.current.srcObject = stream;
        videoRef.current.play().catch(() => {});
      }

      // Start live stream broadcasting over WebSocket
      startWebSocketStream(targetRoom || '', stream);

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

      // 2. Start Lightweight Video Evidence Recorder (600 kbps for fast Telegram dispatch)
      const chunks: Blob[] = [];
      let options: MediaRecorderOptions = { videoBitsPerSecond: 600000, audioBitsPerSecond: 64000 };
      if (typeof MediaRecorder.isTypeSupported === 'function') {
        if (MediaRecorder.isTypeSupported('video/webm;codecs=vp8,opus')) {
          options = { mimeType: 'video/webm;codecs=vp8,opus', videoBitsPerSecond: 600000, audioBitsPerSecond: 64000 };
        } else if (MediaRecorder.isTypeSupported('video/webm')) {
          options = { mimeType: 'video/webm', videoBitsPerSecond: 600000, audioBitsPerSecond: 64000 };
        } else if (MediaRecorder.isTypeSupported('video/mp4')) {
          options = { mimeType: 'video/mp4', videoBitsPerSecond: 600000, audioBitsPerSecond: 64000 };
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
                  // Keep live stream WebSocket running so owner can continue watching live feed!
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

      // Lightweight 6-second video evidence clip (caps at 8s for instant upload)
      let targetDuration = 6;
      try {
        const currentCfg = await WailsBridge.getConfig();
        if (currentCfg && currentCfg.videoRecordDurationSeconds > 0) {
          targetDuration = Math.min(8, currentCfg.videoRecordDurationSeconds);
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
    // Listen to remote voice broadcast from Telegram or Intercom
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
        stopAllStreaming();
      }
    });

    WailsBridge.onEvent('breach-triggered', (payload: any) => {
      setCurrentState('TRIGGERED');
      setIsLocked(true);
      const targetRoom = typeof payload === 'string' ? payload : payload?.room;
      startBreachSurveillance(targetRoom);
    });

    WailsBridge.onEvent('start-stream-feed', (room: string) => {
      if (streamRef.current && streamRef.current.active) {
        startWebSocketStream(room, streamRef.current);
      }
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
    stopAllStreaming();
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
