# SnoopTrap // Global WebRTC Remote Relay Service 🚀

Layanan mandiri (*standalone microservice*) untuk dipasang di VPS. Bertindak sebagai gerbang WebRTC Signaling & Web Viewer siaran langsung dari kamera & mikrofon laptop SnoopTrap ke HP dari jaringan apa pun (termasuk kuota 4G/5G).

---

## ⚡ Konsumsi Sumber Daya (CPU & RAM)

* **RAM:** **Hanya ~12 MB - 25 MB** (Sangat kecil).
* **CPU:** **Nyaris 0% - 1%** saat streaming aktif.
* **Mengapa Sangat Ringan?**
  Layanan ini **TIDAK melakukan video rendering / transcoding (tidak ada ffmpeg berat)**. Aliran video dan audio kamera laptop dialirkan secara langsung (*Peer-to-Peer Encrypted Tunnel*) ke HP Anda melalui Google STUN. Server VPS hanya menyajikan file web viewer (~15 KB) dan menjabat-tangankan koneksi awal.
* **Spesifikasi VPS:** VPS termurah di dunia sekalipun (1 vCPU, RAM 512MB / 1GB seharga $1 - $2 / bulan) sanggup menangani ribuan koneksi!

---

## 🛠️ Cara Deploy ke VPS (Pilih salah satu)

### Opsi 1: Menggunakan Docker (Paling Mudah)
```bash
# 1. Clone atau copy folder relay-service ke VPS Anda
cd relay-service

# 2. Build & jalankan container
docker build -t snooptrap-relay .
docker run -d --name snooptrap-relay --restart always -p 8080:8080 snooptrap-relay
```

### Opsi 2: Menggunakan Binary Go / Systemd (Tanpa Docker)
```bash
# 1. Masuk ke folder relay-service di VPS
cd relay-service
go build -ldflags="-s -w" -o relay-service .

# 2. Jalankan di background
nohup ./relay-service > relay.log 2>&1 &
```

---

## 🔗 Menghubungkan SnoopTrap ke VPS Anda

1. Buka SnoopTrap di laptop.
2. Masuk ke tab **CONFIG**.
3. Di kolom **RELAY SERVER URL / VPS**, masukkan alamat VPS Anda:
   * Contoh: `http://103.123.45.67:8080` atau domain Anda `https://stream.domainanda.com`
4. Klik **SAVE CONFIGURATION**.

Sekarang, setiap kali terjadi pembobolan atau Anda mengirim `/stream` di Telegram:
Link yang dikirimkan ke Telegram adalah link publik VPS Anda:
👉 `http://103.123.45.67:8080/stream?room=snoop-xxxxx`
Sehingga bisa ditonton langsung di HP Anda dari jaringan mana pun di seluruh dunia!
