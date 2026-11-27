# Schakker

Schakker menyediakan aplikasi pengguna dan administrasi terpisah untuk pengelolaan database dan migrasi SQL dengan review, durable queue, worker PostgreSQL dan execution history.

## Mulai development lokal

Dari root repositori, gunakan salah satu launcher berikut dengan Go 1.26+, Node.js 22.12+ / npm dan PostgreSQL server tools terpasang:

```powershell
.\run.bat
```

```bash
bash ./run.sh
```

Launcher utama ini membangun kedua frontend dan menjalankan backend pengguna, backend admin serta worker dengan database lokal terpisah. Buka pengguna di `http://localhost:3000` atau admin di `http://localhost:3001`. Akun awal: `dev.editor`, `dev.reader` dan `dev.admin`; password hasil generate ada di `.schakker/dev/credentials.txt`. Data dan key development tetap tersimpan di `.schakker/dev/`, yang diabaikan Git. Hentikan dengan Ctrl+C atau `run.bat -Stop` / `bash run.sh --stop`.

Jalankan `run.bat -Check` atau `bash run.sh --check` untuk memeriksa login, dua approval leader, enqueue, worker commit dan rollback pada layanan serta database asli, kemudian berhenti.

## Struktur repositori

```text
src/
  apps/
    user/               # Backend pengguna, cmd/server, cmd/worker, frontend
    admin/              # Backend admin, private authority API, frontend
  shared/
    backend/            # Mekanisme Go bersama
    frontend/           # Paket React @schakker/ui
  schemas/
    platform/           # Schema metadata pengguna
    admin/              # Schema metadata admin
  tests/
    unit/{user,admin,shared}/
    integration/
    frontend/
    fixtures/           # Cluster disposable dan sample data target
    k6/
  tools/                # Devserver, load fixture, CodeQL dan CI templates nonaktif
    launchers/          # Implementasi development dan launcher layanan individual
  ops/
    observability/      # Collector, Loki, Tempo, Prometheus dan Grafana
docs/                   # Bukti verifikasi: JSON, SARIF dan screenshot
go.work
package.json
package-lock.json
run.sh / run.bat / run.ps1  # Satu perintah utama untuk seluruh development
```

Seluruh kode aplikasi, library, schema, test, tooling dan operasional berada di `src/`. Masing-masing aplikasi memiliki `backend/`, `database/`, `frontend/`, `go.mod` dan `.env.example`. Go dan npm workspace berada di root repositori. Hanya launcher utama `run` untuk setiap platform yang berada di root; implementasi dan launcher layanan individual berada di `src/tools/launchers/`.

## Database tim dan konfigurasi aplikasi

Untuk memakai database yang sudah disediakan, salin `src/apps/user/.env.example` dan `src/apps/admin/.env.example` menjadi `.env` di aplikasi masing-masing, lalu isi URL, key yang sudah ada dan token layanan yang sesuai. Launcher layanan individual memvalidasi konfigurasi; schema, restricted roles dan akun awal disediakan secara eksplisit di luar startup aplikasi. Schema canonical berada di [src/schemas/platform/schema.sql](src/schemas/platform/schema.sql) dan [src/schemas/admin/schema.sql](src/schemas/admin/schema.sql).

Jalankan pada tiga terminal:

```bash
bash src/tools/launchers/run-admin.sh
bash src/tools/launchers/run.sh
bash src/tools/launchers/run-worker.sh
```

Windows memakai `run-admin.bat`, `run.bat` dan `run-worker.bat` di dalam `src\tools\launchers\`. Konfigurasi pengguna memakai `SCHAKKER_PLATFORM_DATABASE_URL`, `SCHAKKER_MASTER_KEY`, `SCHAKKER_CONTROL_PLANE_URL` dan `SCHAKKER_INTERNAL_TOKEN`; admin memakai `SCHAKKER_ADMIN_DATABASE_URL`, `SCHAKKER_ADMIN_MASTER_KEY` dan token layanan yang sama.

## Build dan test

Semua perintah berikut dijalankan dari root repositori:

```bash
npm ci
npm run typecheck:shared
npm run build

# Modul backend dapat dibangun secara independen, termasuk tanpa go.work.
GOWORK=off go -C src/apps/user build ./...
GOWORK=off go -C src/apps/admin build ./...
GOWORK=off go -C src/tools build ./...

# Semua unit dan integration tests backend dan frontend.
npm test

# Jalankan tiap lapisan secara terpisah.
npm run test:backend:unit
npm run test:backend:integration
npm run test:frontend:unit
npm run test:frontend:integration
```

Unit test frontend memakai Node test runner, esbuild, dan React server rendering tanpa browser. Integration test frontend membangun kedua SPA lalu menguji komponen, routing, formulir, sesi dan interaksi melalui API fixture sintetis di Chrome/Chromium/Edge. Pilih browser dengan `SCHAKKER_QA_BROWSER`; hasil dan screenshot disimpan di `src/tests/output/frontend` atau direktori `SCHAKKER_QA_OUTPUT`.

Integration test backend menjalankan PostgreSQL disposable (perlu `initdb`, `pg_ctl`, dan `psql`) serta MariaDB untuk fitur yang mendukungnya (perlu `mariadbd`, `mariadb-install-db`, dan `mariadb`). Fixture membuat database admin, platform dan target terpisah; tidak memakai data aplikasi development. Unit test backend tidak memerlukan layanan database. Test API backend membuat shell HTML sementara sehingga tidak membutuhkan build frontend. Benchmark batch merupakan pemeriksaan terpisah dengan `SCHAKKER_RUN_BATCH_BENCHMARK=1`.

| Fitur | Unit tests | Integration tests |
| --- | --- | --- |
| Sesi, login, CSRF dan pemisahan aplikasi | Shared/user/admin Go; shared/frontend API dan form | Auth, session rotation, admin separation; browser dua origin |
| Akun karyawan dan administrator | Admin access, HTTP errors; admin frontend | Account HTTP lifecycle, password/session revocation; browser create/update |
| Grup, leader, membership dan permission | Admin policy; group frontend | Groups API, authority/review invalidation; browser membership/grants/delete |
| Koneksi database dan kredensial | Admin connections/security; connection frontend | Duplicate/edit/disable/target isolation; browser create/edit/disable |
| Explorer dan SQL Viewer | Adapter/parser/query builder, display response; frontend results/API | PostgreSQL/MariaDB reads, validation, pagination/sorting; browser explorer/filters/results |
| Visual migration, schema diff dan query plan | Diff/SQL generation/plan parsers; frontend query-plan states | Non-executing analysis API; browser operation builder/diff/tree/clipboard |
| Draft, immutable revision dan approval | Definition validation/hash; frontend API/recovery payloads | Workflow/resources/concurrent approvals; browser draft/review capability checks |
| Queue, worker, batch, retry dan compensation | Queue guards/backoff/worker outcomes; frontend batch SQL/progress | Real worker loop, leases/checkpoints/recovery; browser polling/retry/resume/cancel |
| Execution history, rollback dan forward | Schema dependencies; frontend history/status/recovery | Attempt history, committed ledger, rollback/forward; browser execution selection |
| Startup, schemas dan observability | Config/startup/metrics/tracing | Schema/role provisioning dan startup checks; durable queue → worker trace correlation dan SQL redaction |

Build frontend terpisah memakai `npm run build:user` / `npm run build:admin`. Untuk development Vite, jalankan `npm run dev --workspace=schakker-frontend` atau `npm run dev --workspace=schakker-admin-frontend` (port 5173 / 5174). Proxy API masing-masing tetap menuju server aplikasinya sendiri.

Executable server berada di `src/apps/user/cmd/server` dan `src/apps/admin/cmd/server`; worker di `src/apps/user/cmd/worker`. Launcher mengatur direktori kerja ke root aplikasi agar frontend dan path relatif tetap ditemukan. Perintah provisioning development berada di [devserver](src/tools/cmd/devserver/main.go). Laporan launcher rutin ada di `src/tests/output/`, yang diabaikan Git.

Execution history mencatat seluruh attempt yang selesai, termasuk gagal; hanya commit terverifikasi yang mengubah active/forward stack. SQL analysis dan visual builder tetap tersedia; draf PostgreSQL disimpan melalui definitions/revisions M2.

Validasi PostgreSQL memakai grammar PostgreSQL tanpa menjalankan SQL atau mewajibkan objeknya sudah ada. SQL up dan down diperiksa saat menyimpan dan mengajukan review. Visual builder mempertahankan posisi perubahan row terhadap perubahan schema; dependency antarperubahan schema yang berurutan tetap diurutkan otomatis.

Job dan execution history mencatat pengguna yang menekan Execute, sementara review tetap menyimpan pengajunya. Rollback batch mempertahankan deteksi perubahan eksternal, termasuk UPDATE tanpa perubahan nilai, sambil mendukung rollback berurutan setelah migration berikutnya mengubah row yang sama. Database metadata platform dan admin ditolak sebagai target Explorer, SQL Viewer maupun migration. Role PostgreSQL target memerlukan izin TEMPORARY pada database target untuk verifikasi row selama transaction migration.

Approval mengikuti perubahan authority pada database yang dituju. Pembuatan akun atau perubahan permission pada database lain tidak membatalkan approval. Perubahan leader, membership atau permission yang relevan tetap membatalkannya, termasuk perubahan yang kemudian dikembalikan ke nilai semula.

Untuk batch berstatus partial yang memerlukan approval baru, buka Jobs dan pilih **Request fresh approval to resume**. Semua leader yang diwajibkan saat ini harus menyetujui review baru sebelum job baru dapat diantrekan. Resume memakai scope row dan checkpoint asli, mempertahankan riwayat attempt sebelumnya, dan mencatat pengguna yang mengeksekusi resume. Batch yang sudah mulai dikompensasi tidak dapat dilanjutkan melalui resume.

Statement timeout koneksi worker mengikuti sisa deadline job, terpisah dari timeout SQL Viewer. Migration biasa memakai transaction repeatable read dan snapshot versi row tanpa row lock; scope dipersempit ke tabel yang dirujuk bila aman. SQL dengan efek tidak langsung memakai snapshot konservatif. Perubahan eksternal yang bersamaan tetap menyebabkan konflik atau penolakan rollback.

Log SQL menyertakan struktur query, hash SQL asli, jumlah/tipe parameter, durasi dan SQLSTATE. Literal, komentar dan nilai parameter disamarkan. Log worker menyertakan salinan SQL yang disamarkan serta migration/definition, revision, job, execution, attempt dan batch ID jika relevan; salinan SQL asli tetap tersimpan pada execution history.
