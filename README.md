# Schakker

Untuk langsung menjalankan development lokal dari root repositori pada Windows:

```powershell
.\run-dev.bat
```

Perintah ini menyiapkan database lokal terpisah, akun awal dan key, lalu menjalankan admin (`http://localhost:3001`), aplikasi pengguna (`http://localhost:3000`) dan worker. Login admin `dev.admin`, pengguna `dev.editor`, dan leader View Only `dev.reader`; password hasil generate ada di `src/.schakker/dev/credentials.txt`. Git Bash dapat memakai `bash ./run-dev.sh`. Hentikan dengan Ctrl+C atau `.\run-dev.bat -Stop`. Data development tersimpan untuk pemakaian berikutnya. Lihat [panduan development lokal](docs/m2-integration.md#run-locally-without-team-credentials). Koneksi database tim tetap memakai konfigurasi `.env` tim melalui launcher standar di bawah.

Milestone 2 sudah menghubungkan kedua SPA, authority/group leader, immutable revision dan approval, durable queue, worker PostgreSQL, batch/recovery, serta execution history untuk seluruh attempt yang selesai, termasuk gagal. Buka **Migration workflow** untuk alur draft → review → queue → execution; gunakan admin **Groups** untuk menunjuk leader. Lihat [panduan integrasi dan batasan](docs/m2-integration.md) serta [kontrak API aktual](docs/m2-frontend-api.md).

Source workspace Schakker berada di folder `src/` repositori. Di dalamnya terdapat aplikasi pengguna (`src/src/` dari root repositori), aplikasi administrasi (`src/src-admin/`), library bersama (`src/shared/`), serta konfigurasi Go/npm dan test integrasi. Launcher Bash dan Windows berada di root repositori.

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

Untuk koneksi database tim, database, schema, role PostgreSQL terbatas, akun administrator pertama, key yang sudah ada dan token layanan disediakan di luar aplikasi. Startup aplikasi dan launcher standar hanya memvalidasi konfigurasi tersebut. Launcher development `run-dev` menyediakan lingkungan lokal terpisah seperti dijelaskan di atas.

Panduan startup, kontrak schema, worker, recovery dan verifikasi tersedia di [M2 integration](docs/m2-integration.md).

Salin `src/src/.env.example` dan `src/src-admin/.env.example` menjadi `.env` dalam folder aplikasi masing-masing. Launcher hanya membaca `.env` aplikasi yang dipilih. Binary Go langsung membaca environment, tanpa memuat `.env` otomatis.

Aplikasi pengguna memerlukan `SCHAKKER_PLATFORM_DATABASE_URL`, `SCHAKKER_MASTER_KEY`, `SCHAKKER_CONTROL_PLANE_URL` dan `SCHAKKER_INTERNAL_TOKEN`. URL platform masih menerima alias lama `SCHAKKER_DATABASE_URL` / `DATABASE_URL`. Development dapat membaca key pengguna dari berkas `master.key` yang sudah ada dalam `SCHAKKER_STATE_DIR` (default `.schakker`).

Aplikasi admin memerlukan `SCHAKKER_ADMIN_DATABASE_URL`, `SCHAKKER_ADMIN_MASTER_KEY` dan token layanan yang sama. Key admin tidak dibaca dari aplikasi pengguna. Saat cutover, sediakan key enkripsi koneksi lama sebagai key admin agar ciphertext tetap dapat dibaca. Jangan menghasilkan pengganti secara otomatis.

Listener internal default `127.0.0.1:3002`, dapat diatur dengan `SCHAKKER_ADMIN_INTERNAL_ADDR`. Production (`SCHAKKER_ADMIN_ENV=production`) dan listener non-loopback memerlukan `SCHAKKER_ADMIN_INTERNAL_TLS_CERT` / `SCHAKKER_ADMIN_INTERNAL_TLS_KEY`. Pengguna production (`SCHAKKER_ENV=production`) mewajibkan URL authority HTTPS dengan sertifikat tepercaya. HTTP hanya diizinkan untuk IP loopback pada development. Listener internal tidak diproxy ke browser.

## Menjalankan dan mengembangkan

Gunakan Go 1.26+ (toolchain modul `go1.26.10`), Node.js 22.12+ dengan npm, serta Bash 4+ atau Windows PowerShell 5.1+. Kedua database aplikasi harus sudah disediakan.

Dari root repositori, jalankan admin, aplikasi pengguna dan worker pada tiga terminal terpisah:

```bash
bash ./run-admin.sh
bash ./run.sh
bash ./run-worker.sh
```

Windows menggunakan `run-admin.bat`, `run.bat` dan `run-worker.bat` dari root repositori; PowerShell juga dapat memakai `./run.ps1 -Admin` atau `./run.ps1`; worker memakai `./run.ps1 -Worker`. Launcher dapat dipanggil dari direktori mana pun karena semua path dihitung dari lokasi script. Setiap launcher membangun frontend dan server aplikasinya sendiri, menawarkan pengujian yang terkait, lalu menjalankan binary dalam `bin/`. Fixture integration test tidak diteruskan ke server. Launcher worker membaca `.env` aplikasi pengguna, membangun `cmd/worker` dan menjalankannya tanpa build frontend. Tanpa worker, job tetap pending.

Di dalam source workspace `src/`, kedua aplikasi menggunakan utilitas Go di `shared/backend/` dan paket React `@schakker/ui` di `shared/frontend/`. Tidak ada import antaraplikasi atau konfigurasi runtime bersama. Build source independen memerlukan aplikasi yang dipilih dan folder `shared/` pada layout repositori ini. Binary hasil build tidak memerlukan source shared atau aplikasi lain.

```bash
# Dari root repositori, masuk ke source workspace terlebih dahulu
cd src
# Pilih schakker-admin-frontend atau schakker-frontend
npm ci --workspace=schakker-admin-frontend --workspace=@schakker/ui
npm run build:admin
cd src-admin                 # atau src untuk aplikasi pengguna
GOWORK=off go build -o bin/server .
./bin/server
```

Gunakan `npm run build:user` untuk frontend pengguna. Satu manifest dan lockfile npm, serta `go.work`, berada di source workspace `src/`. Source checkout minimal juga harus menyertakan manifest/lockfile workspace; `replace schakker_shared => ../shared/backend` pada setiap modul Go membuat build tidak bergantung pada `go.work`.

Jalankan `npm run dev` dari `src/src/frontend/` atau `src/src-admin/frontend/` (dari root repositori) untuk pengembangan Vite. Proxy `/api/` dan `/public/` hanya mengarah ke server publik aplikasi tersebut. Route pengguna dan tampilan explorer/migrasi tetap sama. Route admin: `/admin/users`, `/admin/administrators`, `/admin/groups`, `/admin/connections`; `/admin/access` tetap menjadi alias grup.

## Sesi dan permission

Cookie menggunakan HttpOnly, SameSite=Lax, serta Secure pada HTTPS/production. Idle timeout 30 menit, absolute lifetime delapan jam, rotasi berkala 15 menit dan grace period 30 detik tetap berlaku. Password/aktivasi, pengurangan permission dan perubahan koneksi mengubah versi keamanan pengguna secara atomik di authority. Sesi dengan versi lama ditolak pada permintaan berikutnya. Penambahan hak menyebabkan rotasi token saat autentikasi berikutnya. Perubahan permission pengguna tidak membatalkan sesi admin.

## Pengujian

Go unit test, integration test, mock database dan laporan berada di `src/tests/` dari root repositori. Verifikasi browser Person 1 menggunakan fixture API sintetis tanpa dependensi test frontend tambahan: jalankan `npm run verify:person1` dari `src/` setelah build. [Panduan integrasi](docs/m2-integration.md) membedakan pengujian API/database asli dan fixture sintetis untuk verifikasi browser.

```text
src/tests/
  unit/src/           # backend pengguna
  unit/src-admin/     # backend admin
  unit/shared/        # library Go bersama
  integration/        # alur lintas aplikasi dan database
  fixtures/
    website/          # schema/data database aplikasi pengguna
    admin/            # schema/data database administrasi
    postgre/          # data database PostgreSQL target
    mariadb/          # data database MariaDB target
    setup/            # koneksi, local server, role dan fixture lifecycle
  output/             # laporan launcher user/admin
```

Dari root repositori:

```bash
# Semua backend unit + integration tests
GOWORK=off go -C src/tests test -count=1 -timeout=10m ./...
# Hanya unit tests
GOWORK=off go -C src/tests test ./unit/...
```

Go unit suite dapat dipilih melalui `./unit/src/...`, `./unit/src-admin/...`, dan `./unit/shared/...` dari `src/tests/`. `go.work` menyertakan modul test tersebut; production build kedua aplikasi tidak bergantung padanya. npm hanya digunakan untuk instalasi, build dan pengembangan frontend.

Setup database, seed data dan bootstrap akun tersedia dalam test support, yang juga dipakai oleh launcher development eksplisit di `src/tests/cmd/devserver/`. Fixture menyimpan schema database aplikasi saat ini dan data contoh PostgreSQL/MariaDB. PostgreSQL server/client tools diperlukan untuk fixture dan development lokal (`SCHAKKER_POSTGRES_BIN`); MariaDB server tools opsional (`SCHAKKER_MARIADB_BIN`). Launcher standar tetap menawarkan unit/integration test sebelum menjalankan aplikasi, dengan laporan di `src/tests/output/user/` atau `src/tests/output/admin/`. Launcher standar tidak menyiapkan database tim.

## Milestone 2 terintegrasi

Kontrak resource aktual ada di [M2 API](docs/m2-frontend-api.md). Frontend menggunakan ID definition dan revision yang berbeda, UUID untuk execution/job, generation aktif untuk optimistic concurrency, dan `Idempotency-Key` untuk enqueue. Semua leader grup yang punya akses database, termasuk View Only, harus menyetujui setiap aksi. Enqueue menyimpan job dan segera mengembalikan 202; SQL dijalankan oleh worker terpisah.

Schema lengkap saat ini ada di `src/tests/fixtures/website/schema.sql` dan `src/tests/fixtures/admin/schema.sql`. Startup hanya memvalidasi schema, sehingga provisioning/upgrade database tetap dilakukan di luar aplikasi. Jangan menjalankan fixture CREATE TABLE pada database yang sudah berisi data. Script `src/src/database/dao/jobs_upgrade.sql` hanya menambah kolom worker pada schema P2 sebelum integrasi, bukan upgrade umum dari M1.

Batch mendukung penambahan/pengurangan nilai tetap pada kolom BIGINT dengan primary key BIGINT. UI menghasilkan pasangan SQL yang sama dengan `engines.BatchSQL`; pengaturan batch menjadi bagian dari SQL immutable yang disetujui. Execution history mencatat attempt berhasil maupun gagal dengan snapshot SQL yang immutable. Hanya commit terverifikasi yang mengubah active/forward stack; Jobs menampilkan status dan progres. Review tanpa leader tetap Pending Review dan belum dapat masuk queue. Lihat [review spesifikasi dan Q&A](docs/m2-specification-review.md). SQL editor lama tetap tersedia untuk analisis; aksi HTTP `execute` ditolak agar tidak melewati approval.

Batasan dan prosedur recovery ada di [M2 integration](docs/m2-integration.md). Implementasi dan bukti bonus tersedia di [M2 bonuses](docs/m2-bonuses.md): Collector → Loki/Tempo, Prometheus/Grafana, stress test k6 dengan job ber-approval, serta CodeQL Go dan JS/TS. Template GitHub Actions disimpan nonaktif di [temp/workflows](temp/README.md), di luar `.github`; bukti scan lokal tetap tersedia. Bonus pengumpulan cepat tetap membutuhkan pengiriman form sebelum 23 November 2026, 13:10 WIB.

### Hasil stress test bonus

Pengujian lokal menggunakan 5 target VU normal, naik ke 12 lalu 24, kemudian turun dan bertahan di 5. Ketiga skenario berjalan bersamaan: pembacaan API, enqueue idempotent pada database yang sama, dan migration pada dua database lain. Semua 55 job yang diterima selesai melalui worker; HTTP 202 tidak dihitung sebagai migration sukses. API error dan job failure 0%, tetapi threshold API p95 <750 ms **gagal**. Nilai berikut adalah hasil pengukuran, bukan klaim kapasitas production.

| Fase | API p50 / p95 (ms) | Request/s | API error | Queue p95 (ms) | Migration p95 (ms) | Job selesai | Job/s |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Normal, 10 s | 54.86 / 367.26 | 31.40 | 0% | 186.00 | 2707.00 | 9 | 0.90 |
| Load, 15 s | 98.40 / 809.77 | 24.13 | 0% | 210.00 | 3775.00 | 11 | 0.73 |
| Stress, 20 s | 216.44 / 1758.31 | 19.60 | 0% | 215.10 | 5842.45 | 10 | 0.50 |
| Recovery + drain, 32.79 s | 67.39 / 1050.87 | 20.77 | 0% | 196.60 | 5463.80 | 25 | 0.76 |

Normal memenuhi objective latency. Saat load dan stress, p95 meningkat dan throughput request/job menurun walaupun tidak ada kegagalan; menaikkan konkurensi belum meningkatkan kapasitas pada lingkungan ini. Pada recovery, median API mendekati normal dan throughput job membaik, tetapi tail latency belum pulih ke baseline. Fase recovery juga menyelesaikan job yang sudah diterima sebelumnya. Angka queue dan durasi migration dikelompokkan saat job selesai sehingga dapat mencakup pekerjaan dari fase sebelumnya. Fixture menyiapkan approval baru secara serial per database sesuai generation aktif; contention fixture dicatat terpisah. Pengujian singkat di workstation bersama ini belum membuktikan steady-state production atau akar bottleneck. Lihat [script dan cara menjalankan](src/tests/k6/README.md), [JSON hasil asli](docs/evidence/bonuses/k6.json), serta [analisis dan batasan lengkap](docs/m2-bonuses.md).

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
