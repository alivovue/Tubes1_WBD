# Schakker

Schakker terdiri dari aplikasi pengguna dan aplikasi administrasi yang berjalan
sebagai dua server Go dengan dua React SPA.

| Aplikasi | Source | Port HTTP | Port Vite | Launcher |
| --- | --- | --- | --- | --- |
| Pengguna | `src/` | `PORT=3000` | `5173` | `bash ./run.sh` / `run.bat` |
| Administrasi | `src-admin/` | `ADMIN_PORT=3001` | `5174` | `bash ./run-admin.sh` / `run-admin.bat` |

Aplikasi pengguna menyediakan explorer, SQL viewer, query plan, serta migrasi SQL
untuk pengguna yang memiliki grant. Aplikasi admin mengelola akun, grup,
keanggotaan, permission, serta konfigurasi koneksi. Validasi koneksi admin tidak
menyediakan eksekusi SQL atau akses isi database target.

## Database dan konfigurasi

Database, schema, role PostgreSQL, akun administrator pertama, serta master key
harus disediakan melalui pengelolaan database di luar aplikasi. Startup hanya
membuka koneksi dan memeriksa schema serta role yang sudah ada. Startup tidak
menjalankan server PostgreSQL, membuat database/tabel/role, mengisi data contoh,
membuat akun awal, menjalankan upgrade data, atau menghasilkan key file.

- `schakker_platform`: akun dan sesi pengguna biasa, grup, keanggotaan, grant,
  registrasi koneksi, draft migrasi, dan riwayat eksekusi.
- `schakker_admin`: akun, sesi, serta token autentikasi administrator.

Server pengguna tidak membuka koneksi ke database admin. Akun dengan ID atau
username yang sama di kedua database tetap memiliki credential dan sesi terpisah.
Akun pengguna tidak dapat dipromosikan menjadi admin; akun admin dikelola melalui
`/admin/administrators`.

Panduan koneksi dan kontrak schema ada di
[pemisahan database admin](docs/admin-auth-database.md).

Launcher membaca `src/.env`. Launcher admin juga membaca `src-admin/.env` sebagai
override. Berkas `.env` tidak disimpan di Git.

Contoh `src/.env` untuk aplikasi pengguna, dengan nilai rahasia diganti sesuai
konfigurasi deployment:

```dotenv
SCHAKKER_PLATFORM_DATABASE_URL='postgres://schakker_user:PRIVATE_PASSWORD@HOST/schakker_platform?sslmode=verify-full'
SCHAKKER_MASTER_KEY='SHARED_32_BYTE_KEY_ENCODED_AS_HEX_OR_BASE64'
PORT=3000
```

Contoh `src-admin/.env`:

```dotenv
SCHAKKER_PLATFORM_DATABASE_URL='postgres://schakker_administration:PRIVATE_PASSWORD@HOST/schakker_platform?sslmode=verify-full'
SCHAKKER_ADMIN_DATABASE_URL='postgres://schakker_admin_auth:PRIVATE_PASSWORD@HOST/schakker_admin?sslmode=verify-full'
ADMIN_PORT=3001
```

Kedua aplikasi memakai master key yang sama. Key berisi 32 byte, dalam bentuk hex
atau base64. Pada mode development, key juga dapat dibaca dari **berkas yang sudah
ada** di `src/.schakker/master.key` atau `SCHAKKER_STATE_DIR/master.key`. Aplikasi
tidak membuat berkas tersebut. Mode production (`SCHAKKER_ENV=production`)
mewajibkan `SCHAKKER_MASTER_KEY` dari environment.

`SCHAKKER_DATABASE_URL` dan `DATABASE_URL` tetap menjadi alias URL platform.
Server admin selalu membutuhkan `SCHAKKER_ADMIN_DATABASE_URL` tersendiri.
`SCHAKKER_STATE_DIR` hanya menentukan lokasi pembacaan key yang sudah ada; launcher
menafsirkan path relatif dari `src/`.

## Dependensi

- Go 1.26 atau lebih baru; modul menetapkan toolchain `go1.26.10`.
- Node.js 22.12 atau lebih baru beserta npm.
- Bash 4+ di Linux atau Windows PowerShell 5.1+ di Windows.
- Koneksi ke dua database PostgreSQL yang telah disediakan secara eksternal.
- PostgreSQL server tools diperlukan untuk integration test lokal. MariaDB server
  tools bersifat opsional untuk pengujian fixture MariaDB.

Go dan npm mengunduh dependensi sesuai `go.mod`, `go.sum`, dan lockfile masing-masing
frontend. Instal PostgreSQL server tools pada Ubuntu/Debian untuk menjalankan
integration test:

```bash
sudo apt install postgresql postgresql-client
```

`SCHAKKER_POSTGRES_BIN` dapat menunjuk direktori berisi `postgres`, `initdb`, dan
`pg_ctl` untuk **pengujian**. `SCHAKKER_MARIADB_BIN` menentukan tools fixture
MariaDB. Database aplikasi tidak dimulai oleh launcher.

## Menjalankan aplikasi

Dari root repositori, gunakan terminal terpisah:

```bash
bash ./run.sh
bash ./run-admin.sh
```

Pada Windows gunakan `run.bat` dan `run-admin.bat`. Launcher membangun kedua SPA,
menawarkan unit/integration test, membangun Go, lalu menjalankan satu server.
Binary tersimpan di `src/bin/` atau `src-admin/bin/`. Jika schema atau konfigurasi
belum tersedia, server berhenti dengan pesan konfigurasi; tidak ada penyiapan akun
atau data otomatis. Ctrl+C menghentikan server HTTP terkait tanpa mengelola proses
PostgreSQL eksternal.

Pilihan launcher hanya mencakup pengujian. Fixture yang dipakai integration test
bersifat sementara dan tidak diteruskan ke aplikasi yang dijalankan.

Untuk build manual:

```bash
(cd src/frontend && npm ci && npm run build)
(cd src-admin/frontend && npm ci && npm run build)
(cd src && go build -o bin/user-server .)
(cd src-admin && go build -o bin/admin-server .)
```

Jalankan binary dari folder aplikasi terkait agar hasil build frontend ditemukan:
`cd src && ./bin/user-server` atau `cd src-admin && ./bin/admin-server`.

## Pengembangan frontend

Jalankan server Go pengguna pada port `3000`, kemudian `npm run dev` dari
`src/frontend/` untuk membuka Vite pada port `5173`. Untuk admin, jalankan server Go
pada port `3001` dan `npm run dev` dari `src-admin/frontend/` pada port `5174`.
Proxy Vite meneruskan `/api/` dan `/public/` ke server Go terkait. Sesuaikan
`vite.config.ts` jika port Go diubah.

API memakai JSON di `/api/session`, `/api/login`, `/api/logout`, dan `/api` diikuti
route halaman. POST menerima string atau array string dan memerlukan header
`X-CSRF-Token`. Identifier sesi hanya dikirim lewat cookie HttpOnly.

## Sesi dan permission

Cookie pengguna adalah `schakker_session`, sedangkan admin memakai
`schakker_admin_session`, dengan cookie challenge login yang juga terpisah.
Cookie menggunakan `HttpOnly`, `SameSite=Lax`, serta `Secure` pada HTTPS atau
production. Idle timeout 30 menit, absolute lifetime delapan jam, interval rotasi
15 menit, dan grace period 30 detik tetap berlaku.

Logout, perubahan password, dan penonaktifan akun membatalkan sesi pada domain
terkait. Pengurangan grant membatalkan sesi pengguna yang terdampak; perubahan
grant platform tidak membatalkan sesi admin. Permission selalu diperiksa backend.

## Pengujian

Setup database, seed data, bootstrap akun, dan skenario upgrade lama tetap tersedia
khusus untuk pengujian di `src/test/data/`. Kode tersebut tidak menjadi dependensi
kedua aplikasi server.

Bangun kedua frontend sebelum integration test HTTP, lalu jalankan dari root:

```bash
go test -count=1 -timeout=10m ./src/... ./src-admin/...
(cd src/frontend && npm test)
(cd src-admin/frontend && npm test)
```

Pengujian PostgreSQL membuat cluster dan database sementara sendiri. Pengujian
MariaDB dilewati jika tools opsionalnya tidak tersedia. Laporan dari launcher
tersimpan di `src/test/output/`.

## Query Plan

Di halaman SQL Migration, pilih **Analyze migration** atau **Analyze change set**, lalu
buka panel **Query Plan** melalui tautan di bagian atas halaman. Setiap statement SQL
memiliki pohon operasi tersendiri. Klik node induk atau gunakan **Expand all** / **Collapse all**
untuk menelusuri relasi antaroperasi. Panel menampilkan relasi, index, jenis join, estimasi
jumlah baris, startup cost, dan total cost jika tersedia. Nilai yang tidak dikembalikan
database ditandai **Not available**. Cost merupakan satuan estimasi planner, bukan durasi
eksekusi aktual.

**Original EXPLAIN JSON** tersedia sebagai panel sekunder yang tertutup secara bawaan,
dengan tombol **Copy JSON**. Statement yang tidak mendukung EXPLAIN, plan yang tidak
tersedia terhadap skema saat ini, proses loading, dan kegagalan analisis memiliki status
tersendiri. Mengubah SQL membuat plan sebelumnya tidak berlaku sampai analisis diulang.
Menelusuri pohon dan menyalin JSON hanya menggunakan data yang sudah diterima browser.
Backend tetap memakai EXPLAIN tanpa ANALYZE; perubahan ini tidak menambahkan eksekusi SQL.
