---
name: SnoopTrap Desktop
description: Hacker/Cyber Security themed Intruder Detection Lockbox for Windows
colors:
  primary: '#FF003C'
  secondary: '#00FF41'
  background: rgba(10, 10, 10, 0.85)
  surface: '#050505'
  text-primary: '#FFFFFF'
  text-secondary: '#8F8F8F'
  border: '#FF003C'
  warning: '#FF003C'
  surface-dim: '#131313'
  surface-bright: '#3a3939'
  surface-container-lowest: '#0e0e0e'
  surface-container-low: '#1c1b1b'
  surface-container: '#201f1f'
  surface-container-high: '#2a2a2a'
  surface-container-highest: '#353534'
  on-surface: '#e5e2e1'
  on-surface-variant: '#e9bcba'
  inverse-surface: '#e5e2e1'
  inverse-on-surface: '#313030'
  outline: '#af8786'
  outline-variant: '#5f3e3e'
  surface-tint: '#ffb3b2'
  on-primary: '#680012'
  primary-container: '#ff525c'
  on-primary-container: '#5b000f'
  inverse-primary: '#bf002a'
  on-secondary: '#003907'
  secondary-container: '#13ff43'
  on-secondary-container: '#007117'
  tertiary: '#6cd7d8'
  on-tertiary: '#003738'
  tertiary-container: '#2aa0a2'
  on-tertiary-container: '#002f30'
  error: '#ffb4ab'
  on-error: '#690005'
  error-container: '#93000a'
  on-error-container: '#ffdad6'
  primary-fixed: '#ffdad8'
  primary-fixed-dim: '#ffb3b2'
  on-primary-fixed: '#410008'
  on-primary-fixed-variant: '#92001e'
  secondary-fixed: '#72ff70'
  secondary-fixed-dim: '#00e639'
  on-secondary-fixed: '#002203'
  on-secondary-fixed-variant: '#00530e'
  tertiary-fixed: '#8af3f5'
  tertiary-fixed-dim: '#6cd7d8'
  on-tertiary-fixed: '#002020'
  on-tertiary-fixed-variant: '#004f51'
  on-background: '#e5e2e1'
  surface-variant: '#353534'
  background-overlay: rgba(10, 10, 10, 0.85)
  border-alert: '#FF003C'
typography:
  h1:
    fontFamily: '''JetBrains Mono'', ''Fira Code'', monospace'
    fontSize: 3rem
    fontWeight: 700
    lineHeight: 1.1
    letterSpacing: 0.05em
  h2:
    fontFamily: '''JetBrains Mono'', monospace'
    fontSize: 1.5rem
    fontWeight: 600
    lineHeight: 1.2
  body-md:
    fontFamily: '''JetBrains Mono'', monospace'
    fontSize: 1rem
    fontWeight: 400
    lineHeight: 1.5
  code-sm:
    fontFamily: '''JetBrains Mono'', monospace'
    fontSize: 0.75rem
    fontWeight: 400
    color: '{colors.secondary}'
    lineHeight: 16px
    letterSpacing: 0.02em
  headline-xl:
    fontFamily: JetBrains Mono
    fontSize: 48px
    fontWeight: '700'
    lineHeight: 52px
    letterSpacing: 0.05em
  headline-xl-mobile:
    fontFamily: JetBrains Mono
    fontSize: 32px
    fontWeight: '700'
    lineHeight: 38px
    letterSpacing: 0.05em
  headline-lg:
    fontFamily: JetBrains Mono
    fontSize: 24px
    fontWeight: '600'
    lineHeight: 30px
    letterSpacing: 0.02em
  headline-md:
    fontFamily: JetBrains Mono
    fontSize: 20px
    fontWeight: '600'
    lineHeight: 26px
  body-lg:
    fontFamily: JetBrains Mono
    fontSize: 16px
    fontWeight: '400'
    lineHeight: 24px
  body-sm:
    fontFamily: JetBrains Mono
    fontSize: 12px
    fontWeight: '400'
    lineHeight: 16px
  label-md:
    fontFamily: JetBrains Mono
    fontSize: 14px
    fontWeight: '700'
    lineHeight: 18px
    letterSpacing: 0.08em
  label-sm:
    fontFamily: JetBrains Mono
    fontSize: 11px
    fontWeight: '600'
    lineHeight: 14px
    letterSpacing: 0.1em
rounded:
  none: 0px
spacing:
  xs: 4px
  sm: 8px
  md: 16px
  lg: 24px
  xl: 32px
  2xl: 48px
  3xl: 64px
  gutter: 16px
  gutter-desktop: 24px
  margin: 16px
  margin-desktop: 32px
  space-xs: 4px
  space-sm: 8px
  space-md: 16px
  space-lg: 24px
  space-xl: 32px
components:
  panel-terminal:
    backgroundColor: '{colors.surface}'
    border: 1px solid {colors.primary}
    boxShadow: 0 0 20px rgba(255, 0, 60, 0.2)
    padding: '{spacing.2xl}'
  input-otp:
    backgroundColor: transparent
    border: 1px solid {colors.text-secondary}
    textColor: '{colors.primary}'
    padding: '{spacing.md}'
    focusBorder: '{colors.primary}'
    focusShadow: 0 0 10px rgba(255, 0, 60, 0.5)
  button-hack:
    backgroundColor: transparent
    border: 1px solid {colors.primary}
    textColor: '{colors.primary}'
    padding: 12px 24px
    hoverBackground: '{colors.primary}'
    hoverText: '{colors.surface}'
---

## Overview
Desain UI SnoopTrap dirancang khusus untuk menciptakan efek kejut dan intimidasi (The Trap) saat penyusup menyentuh laptop. Tema "Cyber Security / Hacker" digunakan untuk memberikan kesan bahwa sistem sedang melakukan pelacakan balik dan mengambil alih kontrol secara absolut.

## Colors
Warna didominasi oleh Pitch Black dan merah peringatan (#FF003C) untuk menciptakan urgensi. Hijau terminal (#00FF41) digunakan secara spesifik hanya untuk elemen log sistem guna meniru antarmuka terminal peretas sungguhan.

## Typography
Seluruh tipografi diwajibkan menggunakan font monospace (seperti JetBrains Mono atau Fira Code). Tidak ada toleransi untuk font sans-serif biasa.

## Rules to Never Break
- Jangan pernah menggunakan border-radius di atas 0px.
- Jangan gunakan warna putih sebagai latar belakang surface.
- Harus selalu mempertahankan backdrop-blur untuk memberikan konteks bahwa aplikasi asli milik pengguna sedang disandera di belakang layar pelindung.