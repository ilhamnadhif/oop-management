# OPP PCPM HRIS Absensi

MVP aplikasi absensi server-rendered menggunakan Go, HTML template, JavaScript vanilla, dan Google Sheets.

## Fitur MVP

- Register akun dengan password bcrypt.
- Login memakai NRP atau email.
- Logout dan pencatatan aktivitas login.
- Clock in/out menggunakan kamera dan geolocation browser.
- Data tersimpan pada tiga sheet: `user`, `activity login`, dan `absensi data`.
- Foto selfie dikompresi sebagai JPEG dan disimpan sebagai data URI base64 pada spreadsheet.

## Menjalankan lokal

Go 1.26 atau lebih baru diperlukan.

Mode aplikasi selalu menggunakan Google Sheets, termasuk saat dijalankan di lokal. Isi `.env` dengan Spreadsheet ID dan path credential service account, lalu jalankan:

```bash
go run ./cmd/web
```

Aplikasi membaca `.env` dari direktori kerja secara otomatis, jadi tidak perlu `source .env` atau `export`. Aturannya:

- Environment variable yang sudah ada di proses menang atas isi `.env`, sehingga `PORT=9000 go run ./cmd/web` dan env container tetap bisa menimpa.
- Baris kosong, komentar `#`, dan awalan `export` diabaikan.
- Nilai `"..."` dan tanpa kutip mendukung `$VAR`/`${VAR}` (termasuk `$PWD`); nilai `'...'` dipakai apa adanya.
- `.env` yang tidak ada bukan error, ini yang dipakai saat deploy container karena `.env` masuk `.dockerignore`.
- Path file lain bisa dipilih lewat `ENV_FILE=/path/ke/file go run ./cmd/web`.

## Konfigurasi Google Sheets

### Mendapatkan `GOOGLE_SPREADSHEET_ID`

Buka spreadsheet di browser. Dari URL berikut:

```text
https://docs.google.com/spreadsheets/d/1AbCDeFGHIjkLMNopQR/edit#gid=0
```

Nilai di antara `/d/` dan `/edit` adalah Spreadsheet ID:

```text
1AbCDeFGHIjkLMNopQR
```

### Mendapatkan `GOOGLE_APPLICATION_CREDENTIALS`

1. Buka Google Cloud Console dan buat atau pilih project.
2. Masuk ke **APIs & Services → Library**, cari **Google Sheets API**, lalu klik **Enable**.
3. Masuk ke **IAM & Admin → Service Accounts**, klik **Create service account**.
4. Buka service account tersebut, masuk ke tab **Keys → Add key → Create new key → JSON**, lalu download file JSON.
5. Simpan file secara lokal, misalnya `credentials/service-account.json`. Jangan commit file ini ke Git.
6. Salin email service account, biasanya berbentuk `nama-akun@nama-project.iam.gserviceaccount.com`.
7. Buka spreadsheet, klik **Share**, tambahkan email service account tersebut sebagai **Editor**.

`GOOGLE_APPLICATION_CREDENTIALS` adalah path file JSON dari sisi proses yang menjalankan aplikasi, bukan isi JSON-nya:

```bash
export GOOGLE_SPREADSHEET_ID="1AbCDeFGHIjkLMNopQR"
export GOOGLE_APPLICATION_CREDENTIALS="$PWD/credentials/service-account.json"
```

Untuk aplikasi yang berjalan di Google Cloud, lebih aman memakai service account yang terpasang pada runtime/ADC daripada membuat key JSON jangka panjang.

### Menjalankan dengan Google Sheets

```bash
export GOOGLE_SPREADSHEET_ID="1AbCDeFGHIjkLMNopQR"
export GOOGLE_APPLICATION_CREDENTIALS="$PWD/credentials/service-account.json"
export APP_TIMEZONE=Asia/Jakarta
go run ./cmd/web
```

Saat startup aplikasi akan membuat tiga sheet dan header jika belum tersedia. Header yang sudah ada tetapi tidak sesuai akan menyebabkan startup gagal tanpa menimpa data.

Contoh environment variable minimum:

```bash
export GOOGLE_SPREADSHEET_ID="..."
export GOOGLE_APPLICATION_CREDENTIALS="/path/service-account.json"
export APP_TIMEZONE=Asia/Jakarta
export SESSION_COOKIE_SECURE=false
go run ./cmd/web
```

Gunakan `SESSION_COOKIE_SECURE=true` dan HTTPS saat production. Credential JSON jangan dimasukkan ke repository.

## Docker

Build image:

```bash
docker build -t opp-absensi:local .
```

Jalankan dengan credential yang di-mount read-only. Credential tidak disalin ke image:

```bash
docker run --rm -p 8080:8080 \
  -e GOOGLE_SPREADSHEET_ID="1AbCDeFGHIjkLMNopQR" \
  -e GOOGLE_APPLICATION_CREDENTIALS=/run/secrets/google-service-account.json \
  -e SESSION_COOKIE_SECURE=false \
  -v "$PWD/credentials/service-account.json:/run/secrets/google-service-account.json:ro" \
  opp-absensi:local
```

Health check tersedia di `GET /healthz`.

## GitHub Actions CI/CD

Workflow berada di `.github/workflows/ci-cd.yml`.

- Pull request: menjalankan `go test`, race test, `go vet`, build binary, dan validasi Docker build.
- Push ke `main`: semua CI dijalankan lalu image dipublish ke GitHub Container Registry.
- Tag seperti `v1.0.0`: image diberi tag versi semver.
- Image `main` mendapat tag `latest` dan tag commit SHA.
- Credential Google tidak diperlukan oleh pipeline build dan tidak dimasukkan ke image.

Image hasil publish berbentuk:

```text
ghcr.io/<github-owner>/<repository>:latest
ghcr.io/<github-owner>/<repository>:sha-<commit>
```

GitHub Actions menggunakan `GITHUB_TOKEN` dengan permission `packages: write`. Setelah repository dibuat di GitHub, push source ini ke branch `main`:

```bash
git init
git branch -M main
git remote add origin https://github.com/<owner>/<repository>.git
git add .
git commit -m "Initial OPP HRIS Absensi implementation"
git push -u origin main
```

Pipeline ini mem-publish image ke GHCR. Deployment lanjutan ke VPS, Cloud Run, Kubernetes, atau platform lain memerlukan target deployment dan credential runtime yang spesifik.

## Verifikasi

```bash
go test ./...
go vet ./...
go build ./cmd/web
```
