# PRD.md — OPP PCPM HRIS Absensi

## 1. Ringkasan Produk

OPP PCPM HRIS Absensi adalah aplikasi web sederhana untuk mengelola akun pengguna dan absensi harian karyawan. MVP fokus pada fitur login/logout, pendaftaran akun, clock in, dan clock out dengan bukti wajah/selfie serta lokasi pengguna.

Tech stack MVP:

- Backend: Golang
- Frontend: HTML biasa, CSS, JavaScript minimal
- Rendering: server-side render menggunakan `html/template` Golang
- Database: Spreadsheet dengan 3 sheet utama
- Penyimpanan foto: local storage server `/uploads/attendance/...` atau URL file bila memakai storage eksternal

MVP sengaja dibuat sederhana, bukan full HRIS. Fitur payroll, cuti, approval, shift kompleks, dan dashboard admin besar tidak masuk scope awal.

---

## 2. Tujuan

### 2.1 Tujuan Utama

1. User bisa daftar akun dari web.
2. User bisa login menggunakan NRP atau email.
3. User bisa logout.
4. User bisa melakukan clock in dan clock out.
5. Saat clock in/out, sistem wajib mengambil foto wajah/selfie dan lokasi browser.
6. Semua data disimpan ke spreadsheet dengan 3 sheet:
   - `user`
   - `activity login`
   - `absensi data`

### 2.2 Batasan MVP

- Data disimpan di spreadsheet, bukan PostgreSQL/MySQL.
- Frontend tidak memakai React/Vue/Next.js.
- Tidak ada mobile app native.
- Face recognition otomatis belum wajib di MVP. MVP hanya mewajibkan capture foto wajah sebagai bukti absensi.
- Validasi lokasi hanya berdasarkan geolocation dari browser.
- Admin panel lengkap belum wajib. Minimal cukup data tersimpan rapi di spreadsheet.

---

## 3. Role Pengguna

### 3.1 User/Karyawan

User dapat:

- Daftar akun.
- Login.
- Logout.
- Melihat status absensi hari ini.
- Clock in.
- Clock out.

### 3.2 Admin/Super Admin

Untuk MVP awal, admin belum perlu dashboard khusus. Admin bisa melihat dan mengelola data langsung dari spreadsheet.

---

## 4. Fitur Utama

## FR-001 — Register User

User dapat membuat akun melalui halaman daftar.

### Input Form

- Tanggal Gabung
- Nama Lengkap
- NRP
- Jabatan/Posisi
- Email
- Password
- Status Pengguna: Aktif / Tidak Aktif

### Validasi

- NRP wajib unik.
- Email wajib unik.
- Password minimal 8 karakter.
- Tanggal gabung wajib diisi.
- Nama lengkap wajib diisi.
- Status default: Aktif.
- Password tidak boleh disimpan plain text. Harus disimpan sebagai `password_hash`.

### Output

- Data user baru masuk ke sheet `user`.
- Setelah register berhasil, user diarahkan ke halaman login.

---

## FR-002 — Login User

User dapat login menggunakan NRP atau email dan password.

### Input Form

- NRP atau Email
- Password

### Validasi

- Sistem mencari user berdasarkan NRP atau email.
- Password diverifikasi dengan hash.
- User dengan status `Tidak Aktif` tidak boleh login.
- Jika login berhasil, sistem membuat session cookie.
- Jika login gagal, sistem tetap mencatat attempt ke sheet `activity login`.

### Output

- Login sukses diarahkan ke dashboard absensi.
- Login gagal menampilkan error umum: `NRP/Email atau password salah`.
- Aktivitas login tercatat di sheet `activity login`.

---

## FR-003 — Logout User

User dapat logout dari aplikasi.

### Flow

1. User klik tombol Logout.
2. Sistem menghapus session cookie.
3. Sistem mencatat aktivitas logout ke sheet `activity login`.
4. User diarahkan ke halaman login.

---

## FR-004 — Dashboard Absensi

Setelah login, user masuk ke dashboard absensi.

### Informasi yang Ditampilkan

- Nama lengkap user.
- NRP.
- Jabatan.
- Tanggal hari ini.
- Status absensi hari ini:
  - Belum Clock In
  - Sudah Clock In
  - Sudah Clock Out
- Jam clock in jika sudah ada.
- Jam clock out jika sudah ada.

### Tombol

- Clock In: muncul jika user belum clock in hari ini.
- Clock Out: muncul jika user sudah clock in tetapi belum clock out.
- Logout.

---

## FR-005 — Clock In dengan Wajah dan Lokasi

User dapat melakukan clock in dengan mengambil foto wajah/selfie dan lokasi.

### Syarat

- User harus login.
- User harus berstatus aktif.
- User belum melakukan clock in pada tanggal yang sama.
- Browser harus mengizinkan akses kamera dan lokasi.

### Data yang Diambil

- Foto wajah/selfie dari kamera browser.
- Latitude.
- Longitude.
- Accuracy lokasi jika tersedia.
- Waktu clock in dari server.
- IP address.
- User agent.

### Output

- Row baru dibuat di sheet `absensi data`.
- Status absensi menjadi `BELUM_CLOCK_OUT`.

---

## FR-006 — Clock Out dengan Wajah dan Lokasi

User dapat melakukan clock out dengan mengambil foto wajah/selfie dan lokasi.

### Syarat

- User harus login.
- User harus sudah clock in hari ini.
- User belum clock out hari ini.
- Browser harus mengizinkan akses kamera dan lokasi.

### Data yang Diambil

- Foto wajah/selfie dari kamera browser.
- Latitude.
- Longitude.
- Accuracy lokasi jika tersedia.
- Waktu clock out dari server.
- IP address.
- User agent.

### Output

- Row absensi hari itu diperbarui di sheet `absensi data`.
- Status absensi menjadi `SELESAI`.
- Durasi kerja dihitung dalam menit.

---

## 5. Struktur Spreadsheet

Database spreadsheet hanya memakai 3 sheet.

---

## 5.1 Sheet: `user`

Sheet ini menyimpan data akun pengguna.

| Kolom | Tipe | Wajib | Keterangan |
|---|---:|---:|---|
| user_id | string | Ya | UUID internal user |
| tanggal_gabung | date | Ya | Tanggal user bergabung |
| nama_lengkap | string | Ya | Nama lengkap user |
| nrp | string | Ya | Nomor NRP, harus unik |
| jabatan | string | Ya | Jabatan/posisi user |
| email | string | Ya | Email user, harus unik |
| password_hash | string | Ya | Hash password, bukan plain password |
| status_pengguna | string | Ya | `AKTIF` / `TIDAK_AKTIF` |
| created_at | datetime | Ya | Waktu data dibuat |
| updated_at | datetime | Ya | Waktu data terakhir diperbarui |
| last_login_at | datetime | Tidak | Waktu login terakhir |

### Contoh Row

| user_id | tanggal_gabung | nama_lengkap | nrp | jabatan | email | password_hash | status_pengguna | created_at | updated_at | last_login_at |
|---|---|---|---|---|---|---|---|---|---|---|
| usr_01HXX | 2026-08-07 | Budi Santoso | 123456 | Staff Operasional | budi@email.com | `$2a$10$...` | AKTIF | 2026-08-07 14:50:00 | 2026-08-07 14:50:00 | 2026-08-07 15:00:00 |

---

## 5.2 Sheet: `activity login`

Sheet ini menyimpan semua aktivitas login dan logout.

| Kolom | Tipe | Wajib | Keterangan |
|---|---:|---:|---|
| activity_id | string | Ya | UUID aktivitas |
| user_id | string | Tidak | Kosong jika login gagal dan user tidak ditemukan |
| nrp | string | Tidak | NRP user jika tersedia |
| email | string | Tidak | Email user jika tersedia |
| activity_type | string | Ya | `LOGIN` / `LOGOUT` |
| activity_time | datetime | Ya | Waktu aktivitas dari server |
| status | string | Ya | `SUCCESS` / `FAILED` |
| ip_address | string | Tidak | IP client |
| user_agent | string | Tidak | Browser/device user |
| message | string | Tidak | Contoh: `Login sukses`, `Password salah`, `User tidak aktif` |

### Contoh Row

| activity_id | user_id | nrp | email | activity_type | activity_time | status | ip_address | user_agent | message |
|---|---|---|---|---|---|---|---|---|---|
| act_01HXX | usr_01HXX | 123456 | budi@email.com | LOGIN | 2026-08-07 15:00:00 | SUCCESS | 36.x.x.x | Mozilla/5.0 | Login sukses |
| act_01HXY | usr_01HXX | 123456 | budi@email.com | LOGOUT | 2026-08-07 17:00:00 | SUCCESS | 36.x.x.x | Mozilla/5.0 | Logout sukses |

---

## 5.3 Sheet: `absensi data`

Sheet ini menyimpan data clock in dan clock out harian.

| Kolom | Tipe | Wajib | Keterangan |
|---|---:|---:|---|
| absensi_id | string | Ya | UUID absensi |
| user_id | string | Ya | Relasi ke sheet `user` |
| nrp | string | Ya | Snapshot NRP saat absensi |
| nama_lengkap | string | Ya | Snapshot nama saat absensi |
| jabatan | string | Ya | Snapshot jabatan saat absensi |
| tanggal_absensi | date | Ya | Tanggal absensi |
| clock_in_at | datetime | Ya | Waktu clock in dari server |
| clock_in_lat | decimal | Ya | Latitude clock in |
| clock_in_lng | decimal | Ya | Longitude clock in |
| clock_in_accuracy | number | Tidak | Akurasi lokasi dalam meter |
| clock_in_photo | string | Ya | Path/URL foto clock in |
| clock_in_ip | string | Tidak | IP saat clock in |
| clock_out_at | datetime | Tidak | Waktu clock out dari server |
| clock_out_lat | decimal | Tidak | Latitude clock out |
| clock_out_lng | decimal | Tidak | Longitude clock out |
| clock_out_accuracy | number | Tidak | Akurasi lokasi dalam meter |
| clock_out_photo | string | Tidak | Path/URL foto clock out |
| clock_out_ip | string | Tidak | IP saat clock out |
| status_absensi | string | Ya | `BELUM_CLOCK_OUT` / `SELESAI` |
| durasi_menit | number | Tidak | Selisih clock in dan clock out |
| created_at | datetime | Ya | Waktu data dibuat |
| updated_at | datetime | Ya | Waktu data terakhir diperbarui |

### Contoh Row

| absensi_id | user_id | nrp | nama_lengkap | jabatan | tanggal_absensi | clock_in_at | clock_in_lat | clock_in_lng | clock_in_photo | clock_out_at | clock_out_photo | status_absensi | durasi_menit |
|---|---|---|---|---|---|---|---:|---:|---|---|---|---|---:|
| abs_01HXX | usr_01HXX | 123456 | Budi Santoso | Staff Operasional | 2026-08-07 | 2026-08-07 08:01:00 | -6.2001 | 106.8166 | /uploads/attendance/2026-08-07/usr_01HXX_in.jpg | 2026-08-07 17:05:00 | /uploads/attendance/2026-08-07/usr_01HXX_out.jpg | SELESAI | 544 |

---

## 6. Halaman Web

## 6.1 Halaman Login/Register

Route:

- `GET /login`
- `POST /login`
- `GET /register`
- `POST /register`

Tampilan mengikuti desain tab:

- Tab Login
- Tab Daftar

### Login Form

- NRP atau Email
- Password
- Button: Masuk

### Register Form

- Tanggal Gabung
- Nama Lengkap
- NRP
- Jabatan
- Email
- Password
- Status Pengguna
- Button: Daftar Akun

---

## 6.2 Halaman Dashboard Absensi

Route:

- `GET /dashboard`

Isi halaman:

- Info user login.
- Status absensi hari ini.
- Preview kamera.
- Tombol Clock In / Clock Out.
- Tombol Logout.

---

## 6.3 Endpoint Absensi

Route:

- `POST /absensi/clock-in`
- `POST /absensi/clock-out`

Request body menggunakan `multipart/form-data` atau JSON base64.

Rekomendasi MVP:

- Gunakan `multipart/form-data`.
- Field foto: `face_photo`.
- Field lokasi: `latitude`, `longitude`, `accuracy`.

---

## 7. Arsitektur Teknis

## 7.1 Struktur Folder

```txt
app/
├── cmd/
│   └── web/
│       └── main.go
├── internal/
│   ├── handler/
│   │   ├── auth_handler.go
│   │   └── attendance_handler.go
│   ├── service/
│   │   ├── auth_service.go
│   │   └── attendance_service.go
│   ├── repository/
│   │   └── spreadsheet_repository.go
│   ├── session/
│   │   └── session.go
│   └── model/
│       ├── user.go
│       ├── login_activity.go
│       └── attendance.go
├── templates/
│   ├── layout.html
│   ├── login.html
│   ├── register.html
│   └── dashboard.html
├── static/
│   ├── css/
│   │   └── style.css
│   └── js/
│       └── attendance.js
├── uploads/
│   └── attendance/
├── go.mod
└── README.md
```

## 7.2 Komponen

### Handler

Menerima request HTTP, validasi input dasar, render template, dan return response.

### Service

Berisi business logic:

- Register user.
- Login/logout.
- Clock in/out.
- Validasi duplicate clock in.
- Hitung durasi kerja.

### Repository Spreadsheet

Berisi semua operasi baca/tulis spreadsheet:

- Cari user by NRP/email.
- Append row user.
- Append row activity login.
- Cari absensi hari ini.
- Append clock in.
- Update row clock out.

---

## 8. Flow Sistem

## 8.1 Register Flow

1. User membuka halaman register.
2. User mengisi form.
3. Server validasi input.
4. Server cek NRP/email sudah dipakai atau belum.
5. Server hash password.
6. Server append row ke sheet `user`.
7. User diarahkan ke login.

## 8.2 Login Flow

1. User input NRP/email dan password.
2. Server mencari data di sheet `user`.
3. Server cek status user.
4. Server verify password hash.
5. Server membuat session.
6. Server append row ke sheet `activity login`.
7. User masuk dashboard.

## 8.3 Clock In Flow

1. User membuka dashboard.
2. Browser meminta akses kamera dan lokasi.
3. User klik Clock In.
4. Browser mengirim foto wajah dan lokasi ke server.
5. Server validasi user sudah login.
6. Server cek apakah user sudah clock in hari ini.
7. Server simpan foto.
8. Server append row ke sheet `absensi data`.
9. Dashboard menampilkan status `Sudah Clock In`.

## 8.4 Clock Out Flow

1. User membuka dashboard.
2. User klik Clock Out.
3. Browser mengirim foto wajah dan lokasi ke server.
4. Server mencari row absensi hari ini.
5. Server update clock out di row tersebut.
6. Server hitung `durasi_menit`.
7. Status absensi berubah menjadi `SELESAI`.

---

## 9. Validasi dan Business Rules

1. Satu user hanya boleh punya satu row absensi per tanggal.
2. Clock out tidak boleh dilakukan sebelum clock in.
3. Clock in kedua pada hari yang sama harus ditolak.
4. Clock out kedua pada hari yang sama harus ditolak.
5. Waktu absensi menggunakan waktu server, bukan waktu dari browser.
6. Foto wajah wajib ada saat clock in dan clock out.
7. Latitude dan longitude wajib ada saat clock in dan clock out.
8. User tidak aktif tidak boleh login dan tidak boleh absensi.
9. Password wajib disimpan dalam bentuk hash.
10. Semua aktivitas login/logout wajib dicatat.

---

## 10. Keamanan

1. Gunakan password hashing bcrypt/argon2.
2. Jangan simpan password asli di spreadsheet.
3. Session cookie wajib `HttpOnly`.
4. Untuk production, cookie wajib `Secure` dan aplikasi wajib memakai HTTPS.
5. Akses kamera dan lokasi di browser membutuhkan HTTPS, kecuali localhost.
6. File upload foto dibatasi ukuran maksimal, contoh 2 MB.
7. Validasi tipe file hanya image: JPG, JPEG, PNG, WEBP.
8. Spreadsheet credential/API key hanya disimpan di server, tidak boleh dikirim ke frontend.
9. Error login jangan terlalu detail agar tidak membocorkan apakah NRP/email terdaftar.

---

## 11. Acceptance Criteria

## AC-001 — Register

- User bisa membuka halaman daftar.
- User bisa membuat akun baru.
- Data masuk ke sheet `user`.
- NRP/email duplikat ditolak.
- Password tersimpan sebagai hash.

## AC-002 — Login

- User bisa login memakai NRP.
- User bisa login memakai email.
- User tidak aktif tidak bisa login.
- Login berhasil tercatat di sheet `activity login`.
- Login gagal tercatat di sheet `activity login`.

## AC-003 — Logout

- User bisa logout.
- Session hilang setelah logout.
- Aktivitas logout tercatat di sheet `activity login`.

## AC-004 — Clock In

- User login bisa clock in.
- Sistem wajib menerima foto wajah.
- Sistem wajib menerima lokasi.
- Data clock in masuk ke sheet `absensi data`.
- User tidak bisa clock in dua kali pada tanggal yang sama.

## AC-005 — Clock Out

- User yang sudah clock in bisa clock out.
- Sistem wajib menerima foto wajah.
- Sistem wajib menerima lokasi.
- Row absensi hari itu diperbarui.
- Durasi kerja otomatis dihitung.
- User tidak bisa clock out dua kali pada tanggal yang sama.

---

## 12. Prioritas Implementasi

### P0 — Wajib MVP

1. Setup project Golang.
2. Template login/register/dashboard.
3. Session auth.
4. Spreadsheet repository dengan 3 sheet.
5. Register user.
6. Login/logout + activity log.
7. Clock in dengan foto dan lokasi.
8. Clock out dengan foto dan lokasi.
9. Validasi duplicate clock in/out.

### P1 — Setelah MVP Stabil

1. Admin dashboard baca data user dan absensi.
2. Export laporan CSV.
3. Filter absensi per tanggal/user.
4. Batas radius kantor.
5. Approval koreksi absensi.

### P2 — Versi Lanjutan

1. Face recognition/face matching.
2. Anti-spoofing sederhana.
3. Integrasi storage eksternal untuk foto.
4. Migrasi database dari spreadsheet ke PostgreSQL/MySQL.
5. Role admin/super admin lengkap.

---

## 13. Definition of Done

MVP dianggap selesai jika:

1. Aplikasi bisa dijalankan dari Golang server.
2. Login/register tampil sesuai desain dasar.
3. Data user tersimpan di sheet `user`.
4. Login dan logout tercatat di sheet `activity login`.
5. Clock in dan clock out tersimpan di sheet `absensi data`.
6. Foto wajah tersimpan dan path/URL-nya masuk spreadsheet.
7. Lokasi latitude/longitude masuk spreadsheet.
8. Tidak ada password plain text di spreadsheet.
9. User tidak bisa clock in/out ganda pada hari yang sama.
10. Semua halaman utama bisa dipakai dari browser desktop dan mobile.

---

## 14. Catatan Implementasi Penting

- Karena spreadsheet dipakai sebagai database, operasi update clock out perlu menyimpan informasi row/index dari absensi hari itu.
- Nama sheet dengan spasi seperti `activity login` dan `absensi data` tetap bisa dipakai, tetapi query range harus memperhatikan format nama sheet.
- Untuk performa MVP kecil masih aman, tetapi jika data sudah besar sebaiknya pindah ke database SQL.
- Jangan menyimpan foto langsung sebagai base64 panjang di spreadsheet. Lebih baik simpan file di server lalu spreadsheet hanya menyimpan path/URL.
