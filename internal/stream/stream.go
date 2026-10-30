package stream

import (
	"fmt"
	"net"
	"net/http"
	"sync"
)

var (
	currentRoomID string
	roomMu        sync.RWMutex
	serverOnce    sync.Once
)

const viewerHTML = `<!DOCTYPE html>
<html lang="id">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>SnoopTrap // Live Surveillance Feed</title>
  <script src="https://unpkg.com/peerjs@1.5.4/dist/peerjs.min.js"></script>
  <style>
    * { margin:0; padding:0; box-sizing:border-box; }
    body { background:#050505; color:#fff; font-family:monospace; display:flex; flex-direction:column; align-items:center; justify-content:center; min-height:100vh; padding:16px; }
    .card { width:100%; max-width:600px; background:#0a0a0a; border:2px solid #ff003c; border-radius:8px; padding:16px; box-shadow:0 0 25px rgba(255,0,60,0.4); }
    .header { display:flex; justify-content:space-between; align-items:center; border-bottom:1px solid #222; padding-bottom:8px; margin-bottom:12px; font-size:12px; }
    .dot { width:8px; height:8px; background:#ff003c; border-radius:50%; display:inline-block; animation:ping 1s infinite; }
    @keyframes ping { 0%{opacity:1;} 50%{opacity:0.3;} 100%{opacity:1;} }
    video { width:100%; border-radius:4px; background:#000; border:1px solid #222; }
    .status { margin-top:10px; font-size:11px; color:#888; text-align:center; }
  </style>
</head>
<body>
  <div class="card">
    <div class="header">
      <div style="display:flex;align-items:center;gap:6px;"><span class="dot"></span><strong style="color:#ff003c;">SNOOPTRAP LIVE FEED</strong></div>
      <div id="liveBadge" style="color:#00ff41;">CONNECTING...</div>
    </div>
    <video id="liveVideo" autoplay playsinline controls></video>
    <div class="status" id="statusText">Menghubungkan ke kamera & mic laptop...</div>
  </div>
  <script>
    const params = new URLSearchParams(window.location.search);
    const room = params.get('room');
    const statusText = document.getElementById('statusText');
    const liveBadge = document.getElementById('liveBadge');
    const liveVideo = document.getElementById('liveVideo');

    if (!room) {
      statusText.innerText = "Error: Room ID tidak ditemukan.";
    } else {
      const peer = new Peer();
      peer.on('open', () => {
        statusText.innerText = "Memanggil kamera laptop...";
        const call = peer.call(room, new MediaStream());
        call.on('stream', (stream) => {
          liveVideo.srcObject = stream;
          liveVideo.play().catch(()=>{});
          liveBadge.innerText = "[LIVE 30 FPS]";
          statusText.innerText = "Siaran langsung aktif (Video + Audio)";
        });
        call.on('error', (err) => {
          statusText.innerText = "Koneksi terputus: " + err;
        });
      });
      peer.on('error', (err) => {
        statusText.innerText = "Peer error: " + err;
      });
    }
  </script>
</body>
</html>`

// SetCurrentRoomID sets active peerjs room ID
func SetCurrentRoomID(room string) {
	roomMu.Lock()
	defer roomMu.Unlock()
	currentRoomID = room
}

// GetCurrentRoomID gets active peerjs room ID
func GetCurrentRoomID() string {
	roomMu.RLock()
	defer roomMu.RUnlock()
	return currentRoomID
}

// GetLocalIP returns the primary local IPv4 address
func GetLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

// StartStreamServer starts the local HTTP streaming viewer server on port 8765
func StartStreamServer() {
	serverOnce.Do(func() {
		http.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, viewerHTML)
		})
		go func() {
			_ = http.ListenAndServe("0.0.0.0:8765", nil)
		}()
	})
}

