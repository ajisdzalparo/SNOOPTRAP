# SnoopTrap (IntruderLens) 🛡️🚨

> **Personal Intruder Detection System (IDS) & Laptop Guardian for Windows**
> Built with **Wails v2 (Go + React + TypeScript + Tailwind CSS)**.

---

## ⚡ Ringkasan Proyek

SnoopTrap adalah aplikasi pertahanan desktop hybrid untuk melindungi laptop saat ditinggalkan dalam kondisi tidak terkunci (*unlocked*). Begitu sistem mendeteksi input fisik (*mouse movement* atau *keystroke*) saat dalam status **Armed**:

1. **Fullscreen Kiosk Lockbox**: Mengunci layar secara instan (`100vw`, `100vh`) dengan efek `backdrop-filter: blur(20px)` dan memblokir seluruh tombol navigasi sistem (`Alt+Tab`, `Alt+F4`, `Windows Key`, `Ctrl+Esc`) menggunakan Win32 Low-Level Keyboard Hook (`WH_KEYBOARD_LL`).
2. **Targeted Audio Override**: Memaksa unmute dan memutar sirine peringatan dengan volume maksimal (100%) langsung ke speaker internal laptop, mengabaikan headset/earphone.
3. **Silent Intruder Capture**: Mengambil foto wajah penyusup secara instan dan diam-diam via webcam (`navigator.mediaDevices.getUserMedia` + `<canvas>`).
4. **Real-time Telegram 2FA**: Mengirimkan foto wajah penyusup dan 6-digit kode OTP langsung ke Telegram pemilik untuk verifikasi pembukaan kunci.
5. **Anti-Bypass & Nuclear Fail-Safe**: Jika window SnoopTrap kehilangan fokus atau dipaksa tutup sebelum OTP valid dimasukkan, sistem otomatis mengeksekusi `rundll32.exe user32.dll,LockWorkStation` atau `taskkill /IM WhatsApp.exe /F`.

---

## 🏗️ Struktur Arsitektur

```
SNOOPTRAP/
├── build/                 # Wails packaging & build output
│   └── bin/snooptrap.exe  # Production binary
├── docs/                  # PRD dan Design Specifications
│   ├── snooptrap_prd.md
│   ├── gemini-code-1793371086980.md
│   └── gemini-code-1793371091567.txt
├── frontend/              # Web-based Kiosk UI (React 19 + TypeScript + Tailwind v4)
│   ├── src/
│   │   ├── components/
│   │   │   ├── Lockbox.tsx    # Cyber Security / Hacker Lockbox screen
│   │   │   └── Dashboard.tsx  # Command Hub & System Configuration
│   │   ├── services/
│   │   │   └── wailsBridge.ts # Wails Go Binding & Event interface
│   │   ├── App.tsx
│   │   ├── index.css          # Design tokens, neon glow animations, scanline
│   │   └── main.tsx
│   └── vite.config.ts
├── internal/              # Go Core OS & Win32 subsystems
│   ├── audio/             # WASAPI / Volume override & Siren generator
│   ├── config/            # AppData/Roaming/SnoopTrap/config.json persistence
│   ├── failsafe/          # Nuclear Option & LockWorkStation executor
│   ├── hook/              # Win32 WH_KEYBOARD_LL low-level keyboard blocker
│   ├── idle/              # Win32 GetLastInputInfo idle detector
│   └── telegram/          # Telegram Bot API multipart client & polling
├── app.go                 # Core App controller & Wails bindings
├── main.go                # Wails bootstrap, window acrylic & transparency setup
├── wails.json             # Wails project configuration
└── go.mod                 # Go dependencies
```

---

## 🎨 Design Tokens & UI Guidelines (Cyber Security Aesthetic)

- **Alert Primary**: `#FF003C` (Neon Cyber Red)
- **Terminal Secondary**: `#00FF41` (Matrix Green)
- **Surface**: `#050505` (Pitch Black)
- **Background**: `rgba(10, 10, 10, 0.88)` dengan `backdrop-filter: blur(20px)`
- **Typography**: Monospace (`JetBrains Mono`, `Fira Code`)
- **Shapes**: Sharp (`border-radius: 0px` strictly)

---

## 🚀 Cara Menjalankan

### Mode Pengembangan (Live Dev):
```powershell
# Jalankan Wails Dev Server (Hot-reload Go backend & React frontend)
wails dev
```

### Build Binary Eksekutabel Mandiri (Windows Client):
```powershell
wails build
```
Hasil file biner eksekutabel berada di: `build/bin/snooptrap.exe`.

---

## 🌐 Deploy VPS Relay Streaming (Docker)

Hanya modul **Relay Streaming** (`relay-service`) yang dibangun ke dalam Docker. Client desktop SnoopTrap tetap berjalan secara native di Windows.

### Jalankan di VPS:
```bash
# Clone repository di VPS
git clone https://github.com/ajisdzalparo/SNOOPTRAP.git
cd SNOOPTRAP

# Jalankan via Docker Compose
docker compose up -d --build

# ATAU via Docker langsung:
docker build -t snooptrap-stream .
docker run -d --name snooptrap-stream -p 8080:8080 --restart always snooptrap-stream
```

Setelah itu, masukkan **IP VPS** dan **Port 8080** di menu Settings aplikasi SnoopTrap laptop Anda. Link `/stream` Telegram akan langsung bisa dibuka dari jaringan manapun di seluruh dunia!


