import { useState, useEffect, useRef } from 'react';
import Peer from 'peerjs';
import { Lockbox } from './components/Lockbox';
import { Dashboard } from './components/Dashboard';
import { WailsBridge } from './services/wailsBridge';

export function App() {
  const [, setCurrentState] = useState<'DISARMED' | 'ARMED' | 'TRIGGERED'>('DISARMED');
  const [isLocked, setIsLocked] = useState<boolean>(false);
  const [isInitializing, setIsInitializing] = useState<boolean>(true);
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
  const sirenAudioRef = useRef<HTMLAudioElement | null>(null);
  const peerRef = useRef<Peer | null>(null);
  const peerRoomRef = useRef<string>('');

  // Directly route audio to internal laptop speakers (Speakers Realtek) even if headphones are plugged in
  const routeAudioToLaptopSpeaker = async (audioElem: HTMLAudioElement) => {
    try {
      if (typeof (audioElem as any).setSinkId !== 'function') return;
      if (!navigator.mediaDevices || typeof navigator.mediaDevices.enumerateDevices !== 'function') return;

      const devices = await navigator.mediaDevices.enumerateDevices();
      const outputs = devices.filter((d) => d.kind === 'audiooutput');

      // 1. Highest priority: "Speakers (Realtek(R) Audio)" or internal Realtek speaker
      const realtekSpeaker = outputs.find(
        (d) => /speaker/i.test(d.label) && /realtek/i.test(d.label) && !/headphone|headset|cable|virtual/i.test(d.label)
      );
      if (realtekSpeaker && realtekSpeaker.deviceId) {
        await (audioElem as any).setSinkId(realtekSpeaker.deviceId);
        console.log('[AUDIO_ROUTING] Routed to internal laptop speaker:', realtekSpeaker.label);
        return;
      }

      // 2. Second priority: Any physical speaker that is not headphones
      const genericSpeaker = outputs.find(
        (d) => /speaker/i.test(d.label) && !/headphone|headset|cable|virtual/i.test(d.label)
      );
      if (genericSpeaker && genericSpeaker.deviceId) {
        await (audioElem as any).setSinkId(genericSpeaker.deviceId);
        console.log('[AUDIO_ROUTING] Routed to physical speaker:', genericSpeaker.label);
      }
    } catch (err) {
      console.warn('[AUDIO_ROUTING] Failed to route audio to internal speaker:', err);
    }
  };

  const playCyberAlarmSiren = async () => {
    try {
      if (sirenAudioRef.current) {
        sirenAudioRef.current.pause();
        sirenAudioRef.current = null;
      }

      const AudioContextClass = window.AudioContext || (window as any).webkitAudioContext;
      const ctx = new AudioContextClass();
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      const dest = ctx.createMediaStreamDestination();

      osc.type = 'sawtooth';
      const now = ctx.currentTime;
      for (let i = 0; i < 5; i++) {
        osc.frequency.setValueAtTime(800, now + i * 1.0);
        osc.frequency.linearRampToValueAtTime(1800, now + i * 1.0 + 0.5);
        osc.frequency.linearRampToValueAtTime(800, now + i * 1.0 + 1.0);
      }
      gain.gain.setValueAtTime(1.0, now);

      osc.connect(gain);
      gain.connect(dest);

      const audioElem = new Audio();
      audioElem.srcObject = dest.stream;
      audioElem.volume = 1.0;
      await routeAudioToLaptopSpeaker(audioElem);
      await audioElem.play();
      osc.start();
      sirenAudioRef.current = audioElem;

      setTimeout(() => {
        try {
          osc.stop();
          ctx.close();
          if (sirenAudioRef.current === audioElem) {
            sirenAudioRef.current = null;
          }
        } catch {}
      }, 5500);
    } catch (e) {
      console.warn('AudioContext siren error:', e);
    }
  };

  const stopCyberAlarmSiren = () => {
    if (sirenAudioRef.current) {
      try {
        sirenAudioRef.current.pause();
        sirenAudioRef.current.srcObject = null;
      } catch {}
      sirenAudioRef.current = null;
    }
  };

  const capturePhotoOnDemand = async () => {
    try {
      if (!streamRef.current || !streamRef.current.active) {
        const stream = await navigator.mediaDevices.getUserMedia({
          video: { width: 640, height: 480, frameRate: 20 },
          audio: false,
        });
        streamRef.current = stream;
        if (videoRef.current) {
          videoRef.current.srcObject = stream;
          videoRef.current.play().catch(() => {});
        }
        await new Promise((r) => setTimeout(r, 400));
      }

      const v = videoRef.current;
      const c = canvasRef.current;
      if (!v || !c || v.readyState < 2 || v.videoWidth === 0) return;
      c.width = v.videoWidth || 640;
      c.height = v.videoHeight || 480;
      const ctx = c.getContext('2d');
      if (!ctx) return;
      ctx.drawImage(v, 0, 0, c.width, c.height);
      const frameData = c.toDataURL('image/jpeg', 0.8);
      if (frameData) {
        await WailsBridge.processIntruderCapture(frameData, 1, 1);
      }
    } catch (err) {
      console.error('On-demand photo capture failed:', err);
    }
  };

  const recordIntruderAudioOnDemand = async () => {
    try {
      let micStream: MediaStream;
      if (streamRef.current && streamRef.current.getAudioTracks().length > 0) {
        micStream = new MediaStream(streamRef.current.getAudioTracks());
      } else {
        micStream = await navigator.mediaDevices.getUserMedia({ audio: true, video: false });
      }

      let mime = 'audio/ogg';
      if (typeof MediaRecorder.isTypeSupported === 'function') {
        if (MediaRecorder.isTypeSupported('audio/ogg;codecs=opus')) {
          mime = 'audio/ogg;codecs=opus';
        } else if (MediaRecorder.isTypeSupported('audio/webm;codecs=opus')) {
          mime = 'audio/webm;codecs=opus';
        } else if (MediaRecorder.isTypeSupported('audio/mp4')) {
          mime = 'audio/mp4';
        }
      }

      const recorder = new MediaRecorder(micStream, { mimeType: mime, audioBitsPerSecond: 64000 });
      const chunks: Blob[] = [];

      recorder.ondataavailable = (e) => {
        if (e.data && e.data.size > 0) {
          chunks.push(e.data);
        }
      };

      recorder.onstop = () => {
        if (chunks.length > 0) {
          const blob = new Blob(chunks, { type: mime });
          const reader = new FileReader();
          reader.onloadend = () => {
            const b64 = reader.result as string;
            if (b64 && b64.length > 50) {
              WailsBridge.processIntruderAudio(b64).catch(() => {});
            }
          };
          reader.readAsDataURL(blob);
        }
      };

      recorder.start(250);
      setTimeout(() => {
        if (recorder.state === 'recording') {
          recorder.stop();
        }
      }, 10000);
    } catch (err) {
      console.error('On-demand audio recording error:', err);
    }
  };

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

      ws.onclose = () => {
        // Auto-reconnect in 3s if camera stream is still active (e.g. Wi-Fi connecting after forced reboot)
        setTimeout(() => {
          if (streamRef.current && streamRef.current.active && streamWsRef.current === ws) {
            startWebSocketStream(targetRoom, streamRef.current);
          }
        }, 3000);
      };
    } catch (err) {
      console.warn('Failed to start WebSocket streaming:', err);
    }
  };

  // Acquire (or reuse) the live camera + mic MediaStream for streaming.
  const acquireLiveStream = async (): Promise<MediaStream> => {
    if (streamRef.current && streamRef.current.active) {
      return streamRef.current;
    }
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        video: { width: 640, height: 480, frameRate: 20 },
        audio: true,
      });
    } catch (micErr) {
      console.warn('Microphone unavailable, fallback to video-only:', micErr);
      stream = await navigator.mediaDevices.getUserMedia({
        video: { width: 640, height: 480, frameRate: 20 },
        audio: false,
      });
    }
    streamRef.current = stream;
    if (videoRef.current) {
      videoRef.current.srcObject = stream;
      videoRef.current.play().catch(() => {});
    }
    return stream;
  };

  // Register this laptop as a PeerJS peer (ID = stream room) on the public PeerJS cloud,
  // matching the deployed WebRTC viewer. The viewer does peer.call(room); we answer with
  // the live camera + mic stream. Kept registered persistently so the viewer never hits
  // "peer-unavailable" (laptop offline); the camera is only acquired when a call arrives.
  const plog = (m: string) => {
    console.log(m);
    WailsBridge.addFrontendLog(m).catch(() => {});
  };

  const initPeerSurveillance = async () => {
    try {
      const roomId = await WailsBridge.getStreamRoomID();
      if (!roomId) {
        // Wails bindings may not be injected yet at first mount — retry until the room is ready.
        plog('[PEER] Room ID belum siap, retry daftar peer dalam 1.5s...');
        setTimeout(() => initPeerSurveillance(), 1500);
        return;
      }

      // Already registered for this room — nothing to do.
      if (peerRef.current && !(peerRef.current as any).destroyed && peerRoomRef.current === roomId) {
        return;
      }
      if (peerRef.current) {
        try { peerRef.current.destroy(); } catch {}
        peerRef.current = null;
      }

      // Build ICE servers: Google STUN + the VPS TURN relay (from config, never hardcoded).
      // TURN is required when laptop & phone are on different networks (NAT) with no direct
      // P2P path — media is relayed via the VPS.
      const iceServers: any[] = [
        { urls: 'stun:stun.l.google.com:19302' },
        { urls: 'stun:stun1.l.google.com:19302' },
      ];
      const cfg = await WailsBridge.getConfig().catch(() => null);
      if (cfg && cfg.turnUrl && cfg.turnUsername) {
        iceServers.push({ urls: cfg.turnUrl + '?transport=udp', username: cfg.turnUsername, credential: cfg.turnCredential });
        iceServers.push({ urls: cfg.turnUrl + '?transport=tcp', username: cfg.turnUsername, credential: cfg.turnCredential });
        plog('[PEER] TURN relay aktif dari config.');
      } else {
        plog('[PEER] ⚠️ TURN belum diset di config (STUN-only) — bisa gagal konek di jaringan ketat.');
      }

      plog('[PEER] Mendaftarkan laptop sebagai peer, room=' + roomId + ' ...');
      const peer = new Peer(roomId, { config: { iceServers } });
      peerRef.current = peer;
      peerRoomRef.current = roomId;

      peer.on('open', (id) => {
        plog('[PEER] ✅ Laptop ONLINE sebagai peer di PeerJS cloud: ' + id);
      });

      // A phone viewer is calling in — answer with the live camera + mic feed.
      peer.on('call', async (call) => {
        plog('[PEER] 📞 Ada viewer manggil, mengambil kamera+mic & menjawab...');
        try {
          const stream = await acquireLiveStream();
          call.answer(stream);
          plog('[PEER] ✅ Panggilan viewer dijawab dengan stream kamera+mic.');
        } catch (err: any) {
          plog('[PEER] ❌ Gagal ambil kamera/jawab panggilan: ' + (err?.message || err));
        }
      });

      peer.on('disconnected', () => {
        plog('[PEER] ⚠️ Terputus dari broker, reconnect...');
        try { peer.reconnect(); } catch {}
      });

      peer.on('error', (err: any) => {
        const t = err?.type;
        plog('[PEER] ❗ error: ' + t + ' - ' + (err?.message || ''));
        if (t === 'unavailable-id') {
          setTimeout(() => { peerRoomRef.current = ''; initPeerSurveillance(); }, 4000);
        } else if (t === 'network' || t === 'server-error' || t === 'socket-error' || t === 'socket-closed') {
          setTimeout(() => { peerRoomRef.current = ''; initPeerSurveillance(); }, 5000);
        }
      });
    } catch (e: any) {
      plog('[PEER] ❌ init error: ' + (e?.message || e));
    }
  };

  // Live-stream only: acquire the camera on demand (e.g. remote /stream request while
  // DISARMED/ARMED with no breach) and broadcast over WebSocket, WITHOUT capturing the
  // breach photos/10s video evidence that startBreachSurveillance sends to Telegram.
  const startLiveStreamOnly = async (targetRoom?: string) => {
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

      startWebSocketStream(targetRoom || '', stream);
    } catch (e) {
      console.error('Live stream start error:', e);
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

      // Helper function to capture an instant JPEG frame from active webcam stream
      const captureFrame = (): string | null => {
        const v = videoRef.current;
        const c = canvasRef.current;
        if (!v || !c || v.readyState < 2 || v.videoWidth === 0) return null;
        c.width = v.videoWidth || 640;
        c.height = v.videoHeight || 480;
        const ctx = c.getContext('2d');
        if (!ctx) return null;
        ctx.drawImage(v, 0, 0, c.width, c.height);
        return c.toDataURL('image/jpeg', 0.8);
      };

      const captureAndSendPhoto = (photoIdx: number, total: number, retry = 0) => {
        const frame = captureFrame();
        if (frame) {
          WailsBridge.processIntruderCapture(frame, photoIdx, total).catch(() => {});
        } else if (retry < 25) {
          setTimeout(() => captureAndSendPhoto(photoIdx, total, retry + 1), 120);
        }
      };

      // 1. Capture 3 photos separated by 2 seconds each (t=0s, t=2s, t=4s)
      captureAndSendPhoto(1, 3);
      setTimeout(() => captureAndSendPhoto(2, 3), 2000);
      setTimeout(() => captureAndSendPhoto(3, 3), 4000);

      // 2. Start Lightweight Video Evidence Recorder (350 kbps video + 48 kbps audio, 10 seconds ~490 KB MP4 for fast Telegram dispatch)
      const chunks: Blob[] = [];
      let options: MediaRecorderOptions = { videoBitsPerSecond: 350000, audioBitsPerSecond: 48000 };
      if (typeof MediaRecorder.isTypeSupported === 'function') {
        if (MediaRecorder.isTypeSupported('video/mp4;codecs=avc1')) {
          options = { mimeType: 'video/mp4;codecs=avc1', videoBitsPerSecond: 350000, audioBitsPerSecond: 48000 };
        } else if (MediaRecorder.isTypeSupported('video/mp4')) {
          options = { mimeType: 'video/mp4', videoBitsPerSecond: 350000, audioBitsPerSecond: 48000 };
        } else if (MediaRecorder.isTypeSupported('video/webm;codecs=vp8,opus')) {
          options = { mimeType: 'video/webm;codecs=vp8,opus', videoBitsPerSecond: 350000, audioBitsPerSecond: 48000 };
        } else if (MediaRecorder.isTypeSupported('video/webm')) {
          options = { mimeType: 'video/webm', videoBitsPerSecond: 350000, audioBitsPerSecond: 48000 };
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
                  // Keep live stream WebSocket running continuously so owner can watch live feed!
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

      // Lightweight 10-second video evidence clip (~490 KB MP4 for fast Telegram delivery)
      let targetDuration = 10;
      try {
        const currentCfg = await WailsBridge.getConfig();
        if (currentCfg && currentCfg.videoRecordDurationSeconds > 0 && currentCfg.videoRecordDurationSeconds <= 15) {
          targetDuration = currentCfg.videoRecordDurationSeconds;
        }
      } catch {
        // fallback to default 10s
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
    // Listen to remote voice broadcast from Telegram or Intercom (routed directly to laptop speaker)
    WailsBridge.onEvent('play-remote-audio', async (payload: any) => {
      try {
        const mime = payload?.mime || 'audio/ogg';
        const audioSrc = `data:${mime};base64,${payload?.audio}`;
        const audioElem = new Audio(audioSrc);
        audioElem.volume = 1.0;
        await routeAudioToLaptopSpeaker(audioElem);
        audioElem.play().catch((err) => {
          console.error('Remote audio playback failed:', err);
        });
      } catch (err) {
        console.error('Remote audio error:', err);
      }
    });

    // Listen to remote alarm siren command from Telegram
    WailsBridge.onEvent('play-alarm-siren', () => {
      playCyberAlarmSiren();
    });

    // Listen to stop alarm command from Telegram
    WailsBridge.onEvent('stop-alarm-siren', () => {
      stopCyberAlarmSiren();
    });

    // Listen to on-demand photo capture command from Telegram (/foto)
    WailsBridge.onEvent('capture-photo-now', () => {
      capturePhotoOnDemand();
    });

    // Listen to on-demand audio recording command from Telegram (/suara /rekam)
    WailsBridge.onEvent('record-audio-now', () => {
      recordIntruderAudioOnDemand();
    });

    // Register the PeerJS surveillance peer so the remote WebRTC viewer can connect anytime
    initPeerSurveillance();

    // Fetch initial logs & status
    WailsBridge.getLogs().then((initialLogs) => {
      if (initialLogs && initialLogs.length > 0) {
        setLogs(initialLogs);
      }
    });

    WailsBridge.getStatus()
      .then((status) => {
        setCurrentState(status.state as any);
        if (status.state === 'TRIGGERED') {
          setIsLocked(true);
          startBreachSurveillance();
        }
      })
      .finally(() => {
        setIsInitializing(false);
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
      // Ensure the PeerJS peer is registered for this room so the deployed WebRTC viewer
      // (peer.call(room)) can reach us. This is the primary path for the remote relay.
      if (peerRoomRef.current !== room || !peerRef.current || (peerRef.current as any).destroyed) {
        initPeerSurveillance();
      }
      // Also drive the local LAN viewer (port 8765, WebSocket JPEG) if its camera is up.
      if (streamRef.current && streamRef.current.active) {
        startWebSocketStream(room, streamRef.current);
      } else {
        startLiveStreamOnly(room);
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
      if (peerRef.current) {
        try { peerRef.current.destroy(); } catch {}
        peerRef.current = null;
      }
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

      {isInitializing ? (
        <div className="w-screen h-screen bg-[#050505]" />
      ) : isLocked ? (
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
