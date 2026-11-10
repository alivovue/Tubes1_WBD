# Schakker

Schakker memiliki dua aplikasi Milestone 1 yang independen: server Go dan React SPA untuk pengguna di `src/`, serta server dan SPA administrasi di `src-admin/`.

| Aplikasi | Port Go | Port Vite | Launcher |
| --- | --- | --- | --- |
| Pengguna | `PORT=3000` | `5173` | `bash ./run.sh` / `run.bat` |
| Admin | `SCHAKKER_ADMIN_PORT=3001` | `5174` | `bash ./run-admin.sh` / `run-admin.bat` |

Pengguna memiliki explorer, SQL viewer, query plan dan migrasi sesuai permission. Admin mengelola akun, grup, keanggotaan, permission dan registrasi koneksi terenkripsi. Server admin tidak memiliki adapter target, explorer, eksekusi SQL atau mesin migrasi. Registrasi hanya menyimpan konfigurasi; pengujian koneksi admin telah dihapus.

## Kepemilikan database

- `schakker_admin`: akun/password/sesi admin, definisi akun pengguna biasa beserta password, grup, membership, permission, registrasi koneksi dan atribusi administrasi.
- `schakker_platform`: sesi pengguna biasa, draft/eksekusi/riwayat migrasi, namespace receipt, serta snapshot identitas dan ID database untuk foreign key lokal.

Masing-masing server hanya terhubung ke database aplikasinya sendiri. Pengguna memperoleh autentikasi dan permission terkini melalui API internal admin yang dilindungi token layanan dan TLS. Tidak ada cache otorisasi; gangguan authority menolak operasi terlindungi. Snapshot lokal tidak memiliki password, flag administrator atau grant yang dapat diedit.

Akun admin dan pengguna biasa adalah identitas terpisah, termasuk bila username atau ID numeriknya sama. Cookie pengguna `schakker_session` dan cookie admin `schakker_admin_session` juga terpisah. Admin tidak mendapatkan akses database target melalui registrasi atau membership.

## Penyiapan dan konfigurasi

Database, schema, role PostgreSQL terbatas, akun administrator pertama, key yang sudah ada dan token layanan harus disediakan di luar aplikasi. Startup tidak membuat database, schema, role, akun, seed data atau key, dan tidak menjalankan upgrade data.

Panduan lengkap tersedia di:

- [Arsitektur, struktur direktori, API internal, environment dan batasan](docs/admin-auth-database.md)
- [Kontrak schema untuk provisioning eksternal](docs/application-databases.md)
- [Cutover data lama, validasi, retry dan recovery](docs/control-plane-cutover.md)

Isi `src/.env.example` dan `src-admin/.env.example` ke `.env` masing-masing. Launcher hanya membaca `.env` aplikasi yang dipilih. Binary Go langsung membaca environment, tanpa memuat `.env` otomatis.

Aplikasi pengguna memerlukan `SCHAKKER_PLATFORM_DATABASE_URL`, `SCHAKKER_MASTER_KEY`, `SCHAKKER_CONTROL_PLANE_URL` dan `SCHAKKER_INTERNAL_TOKEN`. URL platform masih menerima alias lama `SCHAKKER_DATABASE_URL` / `DATABASE_URL`. Development dapat membaca key pengguna dari berkas `master.key` yang sudah ada dalam `SCHAKKER_STATE_DIR` (default `.schakker`).

Aplikasi admin memerlukan `SCHAKKER_ADMIN_DATABASE_URL`, `SCHAKKER_ADMIN_MASTER_KEY` dan token layanan yang sama. Key admin tidak dibaca dari aplikasi pengguna. Saat cutover, sediakan key enkripsi koneksi lama sebagai key admin agar ciphertext tetap dapat dibaca. Jangan menghasilkan pengganti secara otomatis.

Listener internal default `127.0.0.1:3002`, dapat diatur dengan `SCHAKKER_ADMIN_INTERNAL_ADDR`. Production (`SCHAKKER_ADMIN_ENV=production`) dan listener non-loopback memerlukan `SCHAKKER_ADMIN_INTERNAL_TLS_CERT` / `SCHAKKER_ADMIN_INTERNAL_TLS_KEY`. Pengguna production (`SCHAKKER_ENV=production`) mewajibkan URL authority HTTPS dengan sertifikat tepercaya. HTTP hanya diizinkan untuk IP loopback pada development. Listener internal tidak diproxy ke browser.

## Menjalankan dan mengembangkan

Gunakan Go 1.26+ (toolchain modul `go1.26.10`), Node.js 22.12+ dengan npm, serta Bash 4+ atau Windows PowerShell 5.1+. Kedua database aplikasi harus sudah disediakan.

Jalankan admin dahulu, kemudian aplikasi pengguna, pada terminal terpisah:

```bash
bash ./run-admin.sh
bash ./run.sh
```

Windows menggunakan `run-admin.bat` dan `run.bat`. Setiap launcher membangun frontend dan server aplikasinya sendiri, menawarkan pengujian yang terkait, lalu menjalankan binary dalam `bin/`. Fixture integration test tidak diteruskan ke server.

Kedua aplikasi dapat dibangun sendiri dari folder masing-masing, tanpa aplikasi lain dan tanpa workspace Go:

```bash
# Dalam src-admin/ ATAU src/, dengan environment aplikasi terkait sudah tersedia
(cd frontend && npm ci && npm run build)
GOWORK=off go build -o bin/server .
./bin/server
```

Jalankan `npm run dev` dari frontend aplikasi terkait untuk pengembangan Vite. Proxy `/api/` dan `/public/` hanya mengarah ke server publik aplikasi tersebut. Route pengguna dan tampilan explorer/migrasi tetap sama. Route admin: `/admin/users`, `/admin/administrators`, `/admin/groups`, `/admin/connections`; `/admin/access` tetap menjadi alias grup.

## Sesi dan permission

Cookie menggunakan HttpOnly, SameSite=Lax, serta Secure pada HTTPS/production. Idle timeout 30 menit, absolute lifetime delapan jam, rotasi berkala 15 menit dan grace period 30 detik tetap berlaku. Password/aktivasi, pengurangan permission dan perubahan koneksi mengubah versi keamanan pengguna secara atomik di authority. Sesi dengan versi lama ditolak pada permintaan berikutnya. Penambahan hak menyebabkan rotasi token saat autentikasi berikutnya. Perubahan permission pengguna tidak membatalkan sesi admin.

## Pengujian

Setup database, seed data, bootstrap akun dan simulasi upgrade hanya ada dalam test support. Server tidak mengimpor paket test.

Setelah membangun kedua frontend, jalankan dari root:

```bash
go test -count=1 -timeout=10m ./src/... ./src-admin/...
(cd src/frontend && npm test)
(cd src-admin/frontend && npm test)
```

Admin juga dapat diuji sendiri dari `src-admin/` dengan `GOWORK=off go test ./...`. Cross-application integration test menggunakan workspace hanya untuk menyusun fixture; komunikasi aplikasi tetap melalui API HTTP privat. Tes mencakup menjalankan binary admin terisolasi tanpa folder `src/`, kegagalan authority, pencabutan permission, dan SQL prosedur cutover/recovery yang terdokumentasi.

PostgreSQL server/client tools diperlukan untuk fixture lokal (`SCHAKKER_POSTGRES_BIN` dapat menentukan direktori tools). MariaDB server tools opsional (`SCHAKKER_MARIADB_BIN`). Laporan launcher berada di `test/output/` aplikasi terpilih. Database aplikasi tidak dimulai atau dimigrasikan oleh launcher.

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
