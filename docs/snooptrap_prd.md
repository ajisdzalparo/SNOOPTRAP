# Product Requirements Document (PRD)

**Product Name:** SnoopTrap (Alternative: IntruderLens)
**Company/Project:** Atmarbyte
**Platform:** Windows Desktop Application
**Author:** Ajis Dzalparo
**Document Version:** 1.1 (Comprehensive)
**Date:** October 2026

---

## 1. Executive Summary
SnoopTrap adalah aplikasi *Intruder Detection System* (IDS) skala personal untuk sistem operasi Windows. Berawal dari ide untuk mengamankan chat WhatsApp dari teman yang iseng ("Snoop"), sistem ini berevolusi menjadi **Global Laptop Guardian**. 

Aplikasi ini berjalan sebagai *background service* yang memantau laptop. Ketika laptop ditinggalkan dalam keadaan tidak terkunci dan ada pihak tak berwenang yang menyentuh *mouse* atau *keyboard*, SnoopTrap akan secara instan mengunci layar, membunyikan alarm keras pada *speaker* spesifik (mengabaikan *headset*), memotret wajah penyusup secara diam-diam, dan mengirimkan foto beserta kode OTP (2FA) ke Telegram pemilik untuk membuka kunci.

## 2. Target Pengguna & Use Case
*   **Target Pengguna:** *Software Engineer*, pekerja kantoran, atau individu yang sering bekerja di ruang publik (*coworking space*) atau kantor terbuka.
*   **Use Case Utama (The Trap):** Pengguna pergi ke toilet dan meninggalkan laptop terbuka dengan aplikasi penting (VS Code, WA Desktop, DBeaver) menyala. Rekan kerja iseng mencoba menggerakkan *mouse* untuk membaca layar. Dalam sepersekian detik, layar terblokir total, alarm berbunyi keras menembus seluruh ruangan, wajahnya terfoto, dan pengguna asli menerima notifikasi di HP untuk melakukan intervensi.

---

## 3. Arsitektur Sistem & Tech Stack
Sistem menggunakan pendekatan *Desktop Hybrid* untuk menggabungkan performa *low-level* OS dengan fleksibilitas UI *web-based*.
*   **Core Framework:** **Wails** (Menjembatani OS-level Backend dan Web-based Frontend dalam satu eksekutabel ringan).
*   **Backend:** **Go (Golang)** — Bertugas mengeksekusi Win32 API, manipulasi Windows Core Audio (WASAPI), *state management*, dan komunikasi HTTP ke Telegram.
*   **Frontend:** **React / Next.js** — Bertugas merender UI/UX *Lockbox*, penangkapan *stream webcam*, dan antarmuka pengaturan.
*   **Notifikasi & 2FA:** **Telegram Bot API** — Mengatasi masalah limitasi jaringan (tidak perlu WiFi yang sama antara HP dan Laptop).
*   **Penyimpanan Lokal:** `config.json` di `AppData/Roaming/SnoopTrap/` untuk persistensi preferensi *hardware*.

---

## 4. Functional Requirements (Core Features)

### 4.1. Global Idle Detection & Auto-Arming (Backend Go)
*   **Mekanisme:** Go menggunakan Win32 API (`GetLastInputInfo`) untuk memonitor kapan terakhir kali ada input fisik dari pengguna.
*   **Trigger:** Jika waktu *idle* melewati *threshold* (misal: 3 menit, dapat diatur di *Settings*), *state* internal berubah menjadi `Armed = true`.
*   **Global Scope:** Menghapus pengecekan *Active Window*. Saat `Armed`, pergerakan *mouse* atau tuts *keyboard* sekecil apa pun akan memicu jebakan (terlepas dari aplikasi apa yang sedang dibuka).

### 4.2. Fullscreen Kiosk Lockbox (Frontend React + Win32 API)
*   **Visual Trap:** Wails memanggil *window* baru berukuran absolut (`100vw`, `100vh`), *frameless* (tanpa *border*), dan memaksa status `Always on Top` (`HWND_TOPMOST`) agar menutupi Windows Taskbar dan seluruh aplikasi.
*   **UI/UX:** Layar menampilkan efek *backdrop-filter: blur(20px)* (menyensor konten di belakangnya) dan menampilkan peringatan mencolok (misal: layar merah dengan teks "UNAUTHORIZED ACCESS DETECTED").
*   **Input Blocker:** Go menyuntikkan *Global Keyboard Hook* (`SetWindowsHookEx` dengan `WH_KEYBOARD_LL`) untuk memblokir pintasan sistem: `Alt+Tab`, `Alt+F4`, `Windows Key`, `Ctrl+Esc`. Layar tidak bisa di- *minimize* atau ditutup.
*   **Interaksi:** Hanya tersedia satu *form input* untuk memasukkan kode OTP 2FA. Blokir klik kanan (*context menu*).

### 4.3. Targeted Audio Override System (Backend Go)
Fitur ini menjamin efek jera maksimal meski ada *headset* yang tersambung.
*   **Enumerasi:** Go memanggil Windows Core Audio API (WASAPI) untuk melist seluruh *audio output device* (Speaker, Headset Bluetooth, dll) beserta `Device GUID`-nya. Ditampilkan di UI *Settings* React untuk dipilih pengguna.
*   **Persistence:** Pilihan *speaker* andalan (misal: *Speaker Internal* laptop) disimpan di `config.json`.
*   **The Shock Audio:** Saat jebakan terpicu, Go mengabaikan *Default Playback Device* OS. Go memaksa `Unmute` dan `Volume 100%` pada GUID target, lalu menembakkan *file audio* (sirine/suara robot) **langsung secara spesifik ke *speaker* tersebut**.

### 4.4. Silent Intruder Capture (Frontend React)
*   **Kamera:** Saat UI jebakan muncul, React asinkron memanggil `navigator.mediaDevices.getUserMedia({ video: true })` dalam mode tersembunyi (UI *stream* di-*hide*).
*   **Capture:** Menggunakan elemen `<canvas>` untuk menggambar 1 *frame* instan dan mengonversinya ke *Base64 string*. *(Catatan: Lampu LED hardware webcam akan menyala sekilas, ini dibiarkan sebagai tambahan efek jera).*
*   **Bridge:** React mengirim *string* Base64 ke Go melalui Wails Binding untuk diproses lebih lanjut.

### 4.5. Real-time Telegram 2FA & Remote Gateway (Backend Go)
*   **OTP Generation:** Go membuat 6-digit OTP acak dan menyimpannya di memori sementara.
*   **Pengiriman Data:** Go men-*decode* Base64 menjadi *binary image* dan melakukan HTTP POST *multipart/form-data* ke `https://api.telegram.org/bot<TOKEN>/sendPhoto`.
*   **Payload:** Mengirim foto wajah penyusup, dengan *caption*: `⚠️ ALERT: Ada yang menyentuh laptop lu! Masukkan OTP ini di layar untuk unlock: [KODE OTP]`.
*   **Remote Commands (Opsional):** Go juga melakukan *long-polling* ringan ke Telegram. Jika pengguna membalas chat dengan perintah `/nuke`, Go dapat mengeksekusi penghapusan *session* atau mematikan paksa aplikasi tertentu.

### 4.6. Anti-Bypass & The Nuclear Option (Fail-Safe)
Sistem Windows memiliki celah keamanan *by design* di mana `Ctrl+Alt+Del` tidak dapat diblokir *software*. SnoopTrap membutuhkan mitigasi jika penyusup mencoba membunuh aplikasi lewat Task Manager.
*   **Focus Monitoring:** Go memonitor *foreground window*. Jika *window* SnoopTrap kehilangan fokus (*Lost Focus*) atau mati paksa sebelum OTP yang valid dimasukkan, *Fail-Safe* terpicu.
*   **The Nuclear Action:** Go secara instan mengeksekusi perintah OS: `rundll32.exe user32.dll,LockWorkStation` (Mengunci Windows ke *Lock Screen* bawaan) ATAU `taskkill /IM WhatsApp.exe /F` (jika diatur untuk hanya membunuh aplikasi sensitif).

---

## 5. Skenario Penggunaan (User Journey)
1.  **Setup:** Ajis (pengguna) menginstal SnoopTrap, menghubungkan Token Telegram, dan memilih "Realtek Internal Speaker" di menu *Settings*.
2.  **Idle:** Ajis meninggalkan meja kerja. Setelah 3 menit, SnoopTrap diam-diam masuk ke mode `Armed`.
3.  **Breach:** Teman Ajis yang iseng menggerakkan *mouse*.
4.  **The Trap:**
    *   Layar mendadak berubah merah dan *blur*, menutupi seluruh *chat* dan aplikasi.
    *   Kamera menjepret wajah teman Ajis.
    *   Volume sistem dimaksimalkan, dan suara sirine keras meledak dari *speaker* laptop (meskipun ada *headset* menancap di laptop).
5.  **Notification:** HP Ajis bergetar. Pesan Telegram masuk berisi foto muka panik temannya dan kode OTP `847291`.
6.  **Resolution:** Ajis kembali ke meja, mengetik `847291` di layar merah, sistem tervalidasi, layar merah hilang, dan laptop dapat digunakan kembali seperti biasa.

---

## 6. Non-Functional Requirements
*   **Performance:** *Service background* Go tidak boleh memakan RAM lebih dari 50MB saat *idle*.
*   **Latency:** Penundaan dari pergerakan *mouse* hingga *lock screen* muncul harus < 150ms agar penyusup tidak sempat membaca konten sensitif.
*   **Security & Privacy:** Data (foto dan log) HANYA dikirimkan langsung dari mesin lokal ke *endpoint* API Telegram milik pengguna secara *end-to-end HTTPS*. Aplikasi tidak menyimpan foto ke *disk* (hanya memori) dan tidak mengirim analitik ke pihak ketiga.