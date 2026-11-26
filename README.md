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

Jalankan `run.bat -Check` atau `bash run.sh --check` untuk memeriksa login, dua approval leader, enqueue, worker commit dan rollback pada layanan serta database asli, kemudian berhenti. [Panduan development dan operasi](docs/m2-integration.md) menjelaskan kebutuhan tools, konfigurasi dan recovery.

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
docs/                   # Arsitektur, API, operasi dan bukti verifikasi
go.work
package.json
package-lock.json
run.sh / run.bat / run.ps1  # Satu perintah utama untuk seluruh development
```

Seluruh kode aplikasi, library, schema, test, tooling dan operasional berada di `src/`. Masing-masing aplikasi memiliki `backend/`, `database/`, `frontend/`, `go.mod` dan `.env.example`. Go dan npm workspace berada di root repositori. Hanya launcher utama `run` untuk setiap platform yang berada di root; implementasi dan launcher layanan individual berada di `src/tools/launchers/`. [Arsitektur dan kepemilikan database](docs/architecture.md) menjelaskan batas aplikasi, kode bersama, schema, tools dan local state.

## Database tim dan konfigurasi aplikasi

Untuk memakai database yang sudah disediakan, salin `src/apps/user/.env.example` dan `src/apps/admin/.env.example` menjadi `.env` di aplikasi masing-masing, lalu isi URL, key yang sudah ada dan token layanan yang sesuai. Launcher layanan individual memvalidasi konfigurasi; schema, restricted roles dan akun awal disediakan secara eksplisit di luar startup aplikasi. [Schema canonical](src/schemas/README.md) berada di `src/schemas/platform/schema.sql` dan `src/schemas/admin/schema.sql`.

Jalankan pada tiga terminal:

```bash
bash src/tools/launchers/run-admin.sh
bash src/tools/launchers/run.sh
bash src/tools/launchers/run-worker.sh
```

Windows memakai `run-admin.bat`, `run.bat` dan `run-worker.bat` di dalam `src\tools\launchers\`. Konfigurasi pengguna memakai `SCHAKKER_PLATFORM_DATABASE_URL`, `SCHAKKER_MASTER_KEY`, `SCHAKKER_CONTROL_PLANE_URL` dan `SCHAKKER_INTERNAL_TOKEN`; admin memakai `SCHAKKER_ADMIN_DATABASE_URL`, `SCHAKKER_ADMIN_MASTER_KEY` dan token layanan yang sama. Detail TLS authority dan konfigurasi worker ada di [panduan operasi](docs/m2-integration.md).

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

# Unit dan integration tests; fixture memakai database disposable.
GOWORK=off go -C src/tests test -count=1 -timeout=12m ./...
# Hanya unit test backend pengguna, admin atau mekanisme bersama.
GOWORK=off go -C src/tests test ./unit/user/... ./unit/admin/... ./unit/shared/...

# Browser fixture sintetis; perlu Chrome/Chromium/Edge terpasang.
npm run verify:person1
```

Build frontend terpisah memakai `npm run build:user` / `npm run build:admin`. Untuk development Vite, jalankan `npm run dev --workspace=schakker-frontend` atau `npm run dev --workspace=schakker-admin-frontend` (port 5173 / 5174). Proxy API masing-masing tetap menuju server aplikasinya sendiri.

Executable server berada di `src/apps/user/cmd/server` dan `src/apps/admin/cmd/server`; worker di `src/apps/user/cmd/worker`. Launcher mengatur direktori kerja ke root aplikasi agar frontend dan path relatif tetap ditemukan. Perintah provisioning development berada di [tools](src/tools/README.md). Laporan launcher rutin ada di `src/tests/output/`, yang diabaikan Git.

## Dokumentasi

- [Arsitektur dan batas aplikasi](docs/architecture.md).
- [Setup, workflow, recovery dan keterbatasan](docs/m2-integration.md).
- [Kontrak API frontend](docs/m2-frontend-api.md).
- [Review spesifikasi dan Q&A](docs/m2-specification-review.md).
- [Query Plan dan EXPLAIN](docs/query-plans.md).
- [Bonus, stress test dan bukti verifikasi](docs/m2-bonuses.md).
- [Operasional observability](src/ops/observability/README.md).
- [Review keamanan](docs/m2-security-review.md).

Execution history mencatat seluruh attempt yang selesai, termasuk gagal; hanya commit terverifikasi yang mengubah active/forward stack. SQL analysis dan visual builder tetap tersedia; draf PostgreSQL disimpan melalui definitions/revisions M2. Lihat panduan operasi untuk alur approval, batch, rollback dan recovery.
