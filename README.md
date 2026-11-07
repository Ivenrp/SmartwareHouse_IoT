# Smart Warehouse

Tugas Besar - Sistem Internet of Things (Praktikum IoT TK)

Miniatur gudang dengan lift otomatis 4 level. Barang di-scan lewat QR code, ditimbang, lalu dibawa lift ke rak tujuan. Posisi, berat, dan status lift bisa dipantau lewat dashboard web.

## Anggota Kelompok

| NIM | Nama |
|---|---|
| H1H024004 | Ghozi Itmam Aldani |
| H1H024008 | Mohammad Zulfan Ramadhan |
| H1H024013 | Iven Rival Pangestu |
| H1H024021 | Alma Maida Wirastuti |
| H1H024027 | Yogi Ferdiansyah Amta Miluloh |

## Gambaran Singkat

Sistem terdiri dari tiga bagian yang sudah dikonsultasikan:

- **Layer 1 (perangkat):** ESP32 membaca limit switch L1-L4, HC-SR04, load cell + HX711, dan tombol E-stop. ESP32 juga menggerakkan motor NEMA 17 lewat driver A4988 dan menampilkan status di OLED, LED, dan buzzer.
- **Layer 3 (komunikasi & data):** ESP32 dan backend Golang bertukar pesan lewat MQTT broker (HiveMQ Cloud). Aplikasi klien bicara ke backend lewat HTTP. Data disimpan di MySQL.
- **Layer 4 (aplikasi):** Dashboard web berbasis Golang untuk melihat data real-time dan riwayat, serta menghentikan lift jika perlu.

## Komponen

- ESP32 DevKit
- Stepper NEMA 17 + driver A4988
- Limit switch x4 (L1-L4)
- Sensor jarak HC-SR04
- Load cell 1 kg + modul HX711
- Tombol emergency stop
- OLED, LED, buzzer

## Status Progres

- [x] Rancangan arsitektur Layer 1, 3, dan 4 (sudah konsultasi)
- [x] Penentuan komponen, parameter, dan format data JSON
- [x] Desain awal rangka miniatur
- [ ] Perakitan rangka dan mekanisme lift
- [ ] Wiring dan firmware ESP32
- [ ] Backend Golang (MQTT client + API HTTP)
- [ ] Database MySQL
- [ ] Dashboard web
- [ ] Integrasi dan pengujian

## Rencana Struktur Repository

```
firmware/    kode ESP32
backend/     backend Golang
dashboard/   tampilan web
docs/        dokumen progress dan desain
```
