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
docs/                   # Dokumen pendukung dan bukti QA lainnya
output/                 # Bukti bonus terbaru: scan, metrik, screenshot dan laporan
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

## Penjelasan dan bukti Milestone 2

Bagian ini mengikuti spesifikasi **IF3110 — Milestone 2 — Pengembangan Aplikasi Web**, versi pembaruan yang diberikan, serta sheet **QnA 2**. Nomor halaman di bawah mengacu pada halaman PDF. Penjelasan dan bukti dikumpulkan pada README root ini.

| Ketentuan spesifikasi | Penjelasan dan bukti |
| --- | --- |
| Dataset UPDATE tanpa batching harus membutuhkan lebih dari 1 detik; catat jumlah row dan waktu, lalu gunakan dataset yang sama untuk batch (hlm. 15) | [Pengujian migration batch](#pengujian-migration-batch) |
| Jelaskan pemulihan lock ketika worker gagal (hlm. 16) | [Queue, lock dan pemulihan worker](#queue-lock-dan-pemulihan-worker) |
| Sertakan contoh output log dan span (hlm. 17) | [Activity logging dan tracing](#activity-logging-dan-tracing) |
| Bonus observability: konfigurasi, dashboard dan contoh korelasi log/trace/metrik (hlm. 18) | [Observability terpusat](#observability-terpusat) |
| Bonus k6: script, threshold, hasil dan analisis setiap tingkat beban pada README (hlm. 18–19) | [Stress testing dengan k6](#stress-testing-dengan-k6) |
| Bonus CodeQL: scope, bukti scan, setiap temuan, keputusan, alasan, commit perbaikan dan scan ulang (hlm. 19) | [Security scanning dengan CodeQL](#security-scanning-dengan-codeql) |

Seluruh bukti bonus dibuat ulang pada **27 November 2026 UTC** di [output/](output/). Hasil lama dihapus. [run.json](output/run.json) mencatat perintah, waktu, exit code dan cleanup; [environment.json](output/environment.json) mencatat mesin serta versi tool; [source-manifest.json](output/source-manifest.json) mencatat base commit dan SHA-256 input source, termasuk perubahan yang belum di-commit. Manifest membedakan perubahan saat run dan penyempurnaan tooling setelah pengukuran; hash source input scanner tersedia pada manifest CodeQL. Angka dan threshold yang gagal tetap ditampilkan.

Untuk mereproduksi pengujian pada Linux, sediakan Go, Node.js 22+, Python 3, PostgreSQL server tools, Chrome/Chromium, serta binary resmi CodeQL, k6, Collector Contrib, Loki, Tempo, Prometheus dan Grafana yang sudah diekstrak:

```bash
python3 src/tools/run-bonus-proof.py --tools-directory /path/to/extracted-tools
```

Runner menjalankan scan CodeQL, benchmark batch, lalu k6 bersama stack observability. Database dan storage baru dibuat untuk setiap run; aplikasi development yang sedang berjalan tidak menjadi target beban. Port 3030, 3100, 3200, 3300, 4317–4319, 9090, 9095–9096, 9468 dan 13133 harus kosong. Pilih bagian tertentu dengan `--stage codeql`, `--stage batch` atau `--stage telemetry`; telemetry menjalankan k6 dan menangkap bukti stack penuh. Run parsial mengganti bukti stage yang dipilih serta manifest root; stage lain tetap berasal dari run sebelumnya. Gunakan `SCHAKKER_PROOF_TMPDIR` untuk path temporary yang pendek dengan ruang cukup dan `SCHAKKER_QA_BROWSER` untuk path browser lain. Runner menghentikan layanan dan cluster test miliknya pada akhir run.

`.gitignore` hanya mengizinkan nama file bukti tertentu di `output/`. Input fixture, password/cookie/token, full log, database CodeQL, data PostgreSQL dan storage observability tetap berada di `.schakker/` atau direktori temporary yang diabaikan Git. File tambahan di `output/` juga tetap diabaikan kecuali diizinkan secara eksplisit.

### Queue, lock dan pemulihan worker

API hanya mengantrekan pekerjaan setelah seluruh approval yang diperlukan tersedia, lalu mengembalikan job ID. Tombol eksekusi tetap perlu ditekan pengguna; approval tidak otomatis menjalankan SQL. Self-approval diperbolehkan, tetapi setiap group yang memiliki akses view maupun view+edit tetap mempunyai keputusan tersendiri. Worker memvalidasi ulang permission, authority database, revisi immutable, hash SQL dan posisi migration sebelum melakukan perubahan.

Queue berada pada tabel PostgreSQL `migration_jobs` di database platform. Claim menggunakan `FOR UPDATE SKIP LOCKED`, identitas worker, nomor attempt dan lease. Default worker: concurrency **2**, lease **30 detik**, heartbeat setiap **10 detik**, dan deadline job **30 menit**. Konfigurasi proses tersedia melalui `SCHAKKER_WORKER_CONCURRENCY`, `SCHAKKER_WORKER_LEASE_SECONDS` dan `SCHAKKER_WORKER_TIMEOUT_SECONDS`. Job secara default dibatasi **5 attempt**; kegagalan sementara mendapat exponential backoff 1, 2, 4, 8 detik dan seterusnya, dengan batas jeda 64 detik.

Serialisasi satu database memakai session advisory lock PostgreSQL pada koneksi metadata khusus, dengan key `-database_id`. Lock dipertahankan selama seluruh job, termasuk di antara commit batch. Target PostgreSQL juga memakai session advisory lock `73421108` untuk melindungi eksekusi dan receipt pada target fisik. Database target yang berbeda dapat dikerjakan paralel. Kode utamanya berada pada [database_lock.go](src/apps/user/database/dao/database_lock.go), [jobs.go](src/apps/user/database/dao/jobs.go) dan [runner.go](src/apps/user/backend/worker/runner.go).

Pemulihan ketika worker gagal berlangsung sebagai berikut:

1. Heartbeat yang gagal membatalkan context eksekusi. Guard memeriksa kembali pemilik lease dan nomor attempt sebelum commit. Lease yang kedaluwarsa tidak mengizinkan worker baru mengabaikan advisory lock yang masih dipegang sesi lama.
2. Saat proses atau koneksi berakhir dan PostgreSQL mendeteksinya, PostgreSQL melepaskan session lock. Shutdown normal juga melakukan unlock secara eksplisit. Koneksi yang gagal di-unlock dibuang dari pool.
3. Setelah lease kedaluwarsa, worker lain dapat claim ulang job. Attempt lama yang belum selesai dicatat sebagai `uncertain`; worker baru menunggu database lock sebelum memeriksa target.
4. Worker memeriksa receipt target berdasarkan identitas eksekusi dan hash. Receipt yang sudah committed direkonsiliasi ke metadata/history tanpa menjalankan SQL kembali. Jika hasil commit belum dapat dipastikan, job tetap `uncertain` dan migration berikutnya pada database itu diblokir.
5. Untuk batch yang baru selesai sebagian, sumber kebenaran adalah scope row dan checkpoint pada `_schakker.batch_jobs`, `_schakker.batch_undo` dan `_schakker.batch_receipts`. Perubahan batch dan checkpoint-nya di-commit dalam transaction yang sama. Retry hanya memproses row yang belum committed.

Jika authority berubah dan review lama tidak lagi berlaku, pengguna membuat review resume baru dari Jobs. Semua leader yang diwajibkan saat ini harus menyetujuinya. Job baru melanjutkan scope/checkpoint asli, termasuk penyelesaian receipt bila semua row sudah diproses, tanpa mengulang batch. Attempt lama tetap tercatat. Job `uncertain` harus direkonsiliasi sebelum eksekusi baru; receipt milik job resume yang sudah completed tidak boleh dilewati dengan pembatalan batch.

Pembatalan partial batch menjalankan SQL down asli dalam urutan batch terbalik. Row yang pernah diproses dan versi tuple-nya diperiksa sebelum kompensasi; perubahan eksternal, termasuk no-op UPDATE, dapat menyebabkan pembatalan ditolak. Kegagalan kompensasi menyimpan checkpoint `partially_reverted` agar dapat dilanjutkan. Data yang tidak dapat dipulihkan membutuhkan migration perbaikan baru.

Skenario otomatis terdapat pada [migration_worker_test.go](src/tests/integration/migration_worker_test.go), [batch_resume_approval_test.go](src/tests/integration/batch_resume_approval_test.go), [batch_rollback_order_test.go](src/tests/integration/batch_rollback_order_test.go) dan [batch_version_isolation_test.go](src/tests/integration/batch_version_isolation_test.go).

### Pengujian migration batch

[TestBatchPerformanceEvidence](src/tests/integration/batch_performance_test.go) membuat dataset dengan `generate_series`, mulai dari 100.000 row dan menggandakan jumlahnya sampai satu `UPDATE batch_counters SET value=value+3` membutuhkan lebih dari 1 detik. Batas pencarian dataset adalah 12.800.000 row; test gagal bila baseline belum memenuhi syarat. Setelah baseline, tabel dikosongkan dan diisi ulang dengan ID serta nilai awal yang sama, lalu migration batch dijalankan melalui review, approval, queue dan worker sebenarnya.

Artefak baru [benchmark.json](output/batch/benchmark.json) dan [run.json](output/batch/run.json) mencatat hasil berikut:

| Pengukuran | Hasil run 27 November 2026 |
| --- | ---: |
| Jumlah row / row terdampak | 800,000 / 800,000 |
| UPDATE tanpa batching | 1.715186 detik |
| Ukuran batch | 10,000 row |
| Batch committed / row committed | 80 / 800,000 |
| Durasi melalui worker batch | 130.853491 detik |
| Throughput batch | 6113.71 row/detik |
| Status | `succeeded` |

Lingkungan: **Linux amd64**, AMD Ryzen 7 7840U, 16 logical CPU, Go 1.27.3 dan PostgreSQL 14.24. Detail mesin dan source tersedia pada manifest run di `output/`. Dataset awal kedua pengujian sama; test memeriksa jumlah row committed serta `sum(value) = jumlah_row × 3`.

Baseline memenuhi syarat lebih dari 1 detik. Eksekusi batch lebih lama karena mencakup transaction per batch, penyimpanan scope/checkpoint/undo, validasi dan pelaporan progres. Pengukuran menunjukkan commit bertahap dan hasil data yang benar; waktu ini tidak menunjukkan percepatan terhadap UPDATE tunggal. Pengujian dilakukan pada workstation development bersama.

Benchmark bersifat opt-in dan dilewati oleh integration suite biasa. Runner bukti menjalankannya secara otomatis. Untuk menjalankan benchmark saja dari Bash di root repositori:

```bash
mkdir -p output/batch
SCHAKKER_RUN_BATCH_BENCHMARK=1 \
SCHAKKER_BATCH_REPORT="$PWD/output/batch/benchmark.json" \
go -C src/tests test -count=1 -timeout=30m -v -run '^TestBatchPerformanceEvidence$' ./integration
```

Perintah ini membuat database test disposable. Untuk sekaligus merekam lingkungan, source manifest dan cleanup, gunakan runner dengan `--stage batch`.

### Activity logging dan tracing

Implementasi memakai `log/slog` dengan JSON ke stdout dan exporter span console ke stderr; launcher development mengarahkan keduanya ke `.schakker/dev/{user,admin,worker}.log`. Log operasional tidak disimpan di database platform. Tabel history menyimpan bukti attempt dan committed execution sesuai kebutuhan history, terpisah dari log operasional.

| Jenis catatan | Field dan perilaku |
| --- | --- |
| Aktivitas API/workflow/worker | `timestamp`, `level`, `event`, `status`, `correlation_id`, `trace_id`, `span_id`; `actor_id` dan `database_id` bila identity tersedia pada context |
| SQL | `sql.start` dan `sql.result`, struktur query yang disamarkan, `sql_hash`, jumlah/tipe parameter, `duration_ms`, `affected_rows` bila tersedia, serta `sqlstate` |
| Migration | Definition, revision, review, job, execution, attempt dan batch ID sesuai context eksekusi |
| Transaction | Event commit, rollback dan commit yang belum dapat dikonfirmasi dicatat terpisah dari hasil statement |
| Data sensitif | Password, cookie, token, DSN, literal SQL dan nilai parameter disamarkan sebelum fan-out console/OTLP |

Trace context W3C disimpan pada `migration_jobs.trace_context`, kemudian diekstrak oleh worker. Span menggambarkan API → publish queue → worker → database lock → batch/query → commit. Error tercatat pada event dan status span. Implementasi berada pada [telemetry.go](src/shared/backend/observability/telemetry.go), [sql.go](src/shared/backend/observability/sql.go) dan [runner.go](src/apps/user/backend/worker/runner.go).

Contoh berikut berasal dari run baru di [logs.json](output/observability/logs.json) dan [verification.json](output/observability/verification.json). Cuplikan memilih field yang relevan dari respons Loki; field lain tetap tersedia pada artefak lengkap:

```json
{
  "timestamp_ns": "1795789447063233109",
  "body": "worker.finished",
  "attributes": {
    "event": "worker.finished",
    "status": "succeeded",
    "job_status": "succeeded",
    "job_id": "25781592-5c61-413b-8649-6bcd5b9c735d",
    "attempt": "1",
    "service_name": "schakker-load-fixture",
    "trace_id": "7c02681ddaa7392f625971a8bc44bd96",
    "span_id": "4d6c15da9d434226",
    "sql_snapshot": "CREATE TABLE public . load_7 ( id bigint )"
  }
}
```

Span worker dari [trace.json](output/observability/trace.json), dengan ID base64 sesuai JSON OTLP:

```json
{
  "traceId": "fAJoHdqnOS9iWXGovES9lg==",
  "spanId": "TWwV2p1DQiY=",
  "parentSpanId": "tGnMOx0Qi8o=",
  "name": "worker.execute",
  "startTimeUnixNano": "1795789446086760152",
  "endTimeUnixNano": "1795789447063684768"
}
```

ID base64 tersebut menghasilkan trace ID hex `7c02681ddaa7392f625971a8bc44bd96` dan span ID `4d6c15da9d434226`, sama dengan log. Durasi span sekitar **976.92 ms**. Trace lengkap berisi **182 span**, termasuk enqueue, authority, queue publish, lock, database dan worker. Test [OTLP/metrics](src/tests/unit/shared/observability/otlp_metrics_test.go) memeriksa export/redaction; [worker_telemetry_test.go](src/tests/integration/worker_telemetry_test.go) memeriksa propagasi melalui durable queue dan worker.

### Observability terpusat

Alur konfigurasi bonus adalah:

```text
Go JSON logs + traces → OTLP HTTP :4318 → Collector → Loki (logs) / Tempo (traces)
Go /metrics :9464, :9465, :9466 → Prometheus :9090 → Grafana :3030
Proof fixture /metrics :9468 → Prometheus khusus run yang sama
```

Collector, storage dan dashboard dapat dijalankan ulang memakai [compose.yaml](src/ops/observability/compose.yaml), [collector.yaml](src/ops/observability/collector.yaml), [prometheus.yml](src/ops/observability/prometheus.yml), [Loki](src/ops/observability/loki.yaml), [Tempo](src/ops/observability/tempo.yaml), [datasource provisioning](src/ops/observability/grafana/provisioning/datasources/schakker.yaml) dan [dashboard JSON](src/ops/observability/grafana/dashboards/migrations.json).

Pilihan Docker Compose dari root repositori:

```bash
bash src/ops/observability/init.sh
docker compose -f src/ops/observability/compose.yaml up -d
```

Konfigurasi memakai host networking dan listener loopback; Docker Desktop perlu mendukung serta mengaktifkan host networking. Alternatif yang digunakan untuk bukti tersimpan adalah binary portable resmi Collector, Loki, Tempo, Prometheus dan Grafana, bukan Compose:

```bash
python3 src/ops/observability/run-local.py --data-directory .schakker/observability
```

Perintah portable memerlukan binary pada PATH; `--tools-directory /path/to/extracted-tools` dapat digunakan untuk arsip release yang sudah diekstrak. Grafana tersedia di `http://127.0.0.1:3030`, username `admin`; password generate disimpan pada direktori data atau file secret yang dibuat `init.sh` untuk Compose.

Jalankan aplikasi pada terminal lain dengan endpoint per-signal berikut. Launcher utama menonaktifkan endpoint OTLP umum secara default; variabel per-signal ini tetap diteruskan ke ketiga proses:

```bash
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://127.0.0.1:4318/v1/traces \
OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://127.0.0.1:4318/v1/logs \
bash ./run.sh
```

Endpoint Prometheus development adalah pengguna `127.0.0.1:9464/metrics`, admin `127.0.0.1:9465/metrics` dan worker `127.0.0.1:9466/metrics`. Untuk proses yang dikonfigurasi sendiri, gunakan `SCHAKKER_METRICS_ADDR`; `SCHAKKER_METRICS_TOKEN` melindungi listener yang memerlukan token.

| Metrik | Kegunaan |
| --- | --- |
| `schakker_jobs{operation,status}` | Jumlah durable job per status, termasuk pending/running/succeeded/failed dan status recovery |
| `schakker_job_retries_total`, `schakker_migration_attempts_total` | Retry dan attempt selesai per outcome |
| `schakker_queue_wait_seconds`, `schakker_database_lock_wait_seconds` | Waktu tunggu queue pertama dan akuisisi lock |
| `schakker_migration_duration_seconds` | Durasi attempt worker, termasuk lock wait dan recovery |
| `schakker_migration_batches`, `schakker_migration_rows` | Proyeksi batch/row committed; tersedia pula metrik reverted batch/row |
| `schakker_database_pool_connections{pool,state}` | Koneksi active/idle pada pool aplikasi platform/admin |
| `schakker_database_pool_wait_seconds_total`, `schakker_database_pool_wait_total` | Total durasi dan jumlah tunggu koneksi pool |
| `schakker_metrics_collection_success{source="queue"}` | Keberhasilan pembacaan metrik queue dari metadata |

Label metrik dibatasi pada operation/status/kind/pool/state/method/status class yang dikenal. User ID, job ID dan SQL tidak menjadi label time series. `trace_id` dipakai sebagai **exemplar**. Konfigurasi Loki mengindeks resource attribute `service.name` saja; identitas job/trace menjadi structured metadata. API query Loki dapat memproyeksikan metadata tersebut ke map `stream` pada respons; kemunculannya di respons itu tidak berarti semuanya menjadi indexed label.

[verification.json](output/observability/verification.json) membuktikan log `worker.finished`, trace Tempo dan exemplar Prometheus memakai trace ID **`7c02681ddaa7392f625971a8bc44bd96`** yang sama. Exemplar `schakker_migration_duration_seconds_bucket` bernilai **0.976820387 detik**; trace lengkap memuat **182 span**. [metrics.json](output/observability/metrics.json) menyimpan respons Prometheus dan exemplar; [metrics.prom](output/observability/metrics.prom) menyimpan snapshot endpoint. Query final menunjukkan **52 job `up/succeeded`**, sesuai completed job k6, serta pool active/idle dan waktu tunggu koneksi.

Dashboard `schakker-migrations` dengan **12 panel** berhasil diprovision, direkam melalui [dashboard.json](output/observability/dashboard.json), dan ditangkap pada [screenshot Grafana baru](output/observability/grafana.png). [config.json](output/observability/config.json) menyimpan konfigurasi scrape khusus run ini dan referensi konfigurasi stack yang dipakai.

Runner [run-bonus-proof.py](src/tools/run-bonus-proof.py) menjalankan seluruh stack portable dengan storage baru, memastikan log worker dan exemplar Prometheus mempunyai trace ID yang sama, mengambil trace itu dari Tempo dan memeriksa span `worker.execute`, lalu memeriksa provisioning dashboard melalui API Grafana. [capture-grafana.mjs](src/tools/capture-grafana.mjs) login pada Grafana lokal melalui profile browser temporary dan menangkap dashboard yang dirender. Konfigurasi scrape proof memakai port 9468 saja; data layanan development pada 9464–9466 tidak tercampur. Compose dan Windows tidak dijalankan pada pengujian stack Linux ini.

### Stress testing dengan k6

[stress.js](src/tests/k6/stress.js) menjalankan tiga scenario `ramping-vus` secara bersamaan: traffic API, submission migration ke database yang sama, dan submission ke database berbeda. [loadfixture](src/tools/cmd/loadfixture/main.go) menyediakan tiga target PostgreSQL disposable, authority/API nyata, approval leader dan worker concurrency **4**. Dataset, database dan credentials fixture dipisahkan dari aplikasi development.

[migration_jobs.js](src/tests/k6/migration_jobs.js) meminta review yang telah mendapat approval untuk head database saat itu, lalu mengirim dua enqueue request bersamaan dengan idempotency key yang sama. Keduanya harus mengembalikan job ID yang sama. Script kemudian polling sampai job benar-benar selesai; HTTP 202 tidak dihitung sebagai keberhasilan migration. Persiapan review pada target yang sibuk dapat mengembalikan 409 contention dan dicoba kemudian; request kontrol fixture ini dipisahkan dari metrik API aplikasi.

[helpers.js](src/tests/k6/helpers.js) mencatat latency/request/error API, accepted/completed job, queue wait, durasi migration dan waktu penyelesaian job. Queue wait dihitung dari `started_at - created_at`; durasi migration dari `completed_at - started_at`. Timestamp tersebut berasal dari proyeksi job yang diperiksa lewat API. Completed job diberi label fase saat selesai, sehingga recovery dapat mencakup job yang diajukan pada fase sebelumnya. Request throughput adalah request aplikasi yang masuk `record()`, bukan seluruh request kontrol fixture atau login.

Target beban default pada [config.js](src/tests/k6/config.js) dan [stress.js](src/tests/k6/stress.js):

| Fase | Durasi konfigurasi | Target VU API / database sama / database berbeda | Target total VU |
| --- | ---: | --- | ---: |
| normal | 10 detik | 2 / 1 / 2 | 5 |
| load | 15 detik | 6 / 2 / 4 | 12 |
| stress | 20 detik | 12 / 4 / 8 | 24 |
| recovery | 30 detik | 2 / 1 / 2 | 5 |

Nilai tersebut merupakan target ramp, bukan beban konstan sepanjang fase. Recovery menurunkan beban selama 10 detik pertama lalu mempertahankan target normal. Graceful stop memberi waktu bagi pekerjaan yang masih berjalan untuk selesai.

Threshold global pada script adalah **API p95 < 750 ms**, **API error rate < 1%**, **job failure rate < 1%**, dan sedikitnya satu job completed. **750 ms merupakan default eksperimen pada kode, bukan angka wajib spesifikasi M2 atau SLA yang sudah disepakati.** Spesifikasi meminta tim menetapkan dan menjelaskan threshold p95 dan error rate. Target eksperimen ini dipertahankan pada laporan meskipun gagal; pemilihan target untuk lingkungan lain perlu didasarkan pada baseline dan kebutuhan yang disepakati sebelum pengujian.

Default dapat dikonfigurasi melalui `SCHAKKER_K6_API_P95_MS`, `SCHAKKER_K6_API_ERROR_RATE`, `SCHAKKER_K6_STAGE_SECONDS` (empat durasi) dan `SCHAKKER_K6_JOB_TIMEOUT_SECONDS` (default polling 60 detik). Threshold per fase p95 < 10 detik, error rate < 100%, serta queue/migration p95 < 120 detik adalah pemeriksaan batas tambahan yang longgar; lolosnya pemeriksaan itu tidak menggantikan threshold global 750 ms dan 1%.

Untuk membuat proof k6 dan observability secara bersamaan:

```bash
python3 src/tools/run-bonus-proof.py --tools-directory /path/to/extracted-tools --stage telemetry
```

Runner mempertahankan fixture hingga korelasi telemetry dan screenshot selesai, lalu menghentikan seluruh layanan miliknya. Summary publik hanya mengekspor state test, metric dan metadata stage; credential fixture tetap private. Untuk workload k6 saja, tersedia `SCHAKKER_K6_OUTPUT="$PWD/src/tests/output/k6" bash src/tools/run-k6.sh`. Output wrapper itu tetap ignored dan bukan bukti yang dipakai pada laporan ini.

Hasil mentah baru ada di [summary.json](output/k6/summary.json), threshold/exit code di [run.json](output/k6/run.json), dan laporan terhitung di [results.md](output/k6/results.md). Render ulang tabel tanpa menjalankan test:

```bash
python3 src/tests/k6/report.py output/k6/summary.json
```

**Lingkungan:** Linux x86_64, AMD Ryzen 7 7840U, 16 logical CPU, Go 1.27.3, PostgreSQL 14.24 dan k6 2.4.0. Detail pada [environment.json](output/environment.json). Ini workstation development bersama; stack observability ikut berjalan saat pengukuran, sementara scan CodeQL dan benchmark sudah selesai.

| Phase | Duration | API p50 / p95 (ms) | API req/s | API errors | Queue p95 (ms) | Migration p95 (ms) | Completed jobs | Job/s |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| normal | 10.00s | 70.75 / 140.78 | 34.20 | 0.00% | 287.00 | 4479.40 | 9 | 0.90 |
| load | 15.00s | 108.18 / 397.81 | 30.00 | 0.00% | 314.00 | 5248.60 | 12 | 0.80 |
| stress | 20.00s | 303.86 / 1081.84 | 23.05 | 0.00% | 311.40 | 10059.20 | 9 | 0.45 |
| recovery | 34.40s | 115.67 / 916.13 | 20.17 | 0.00% | 258.25 | 10042.70 | 22 | 0.64 |

Durasi recovery terukur mencakup graceful stop; keseluruhan run **79.40 detik**. Tercatat **1,947 request API**, **52 job accepted dan 52 completed**, tanpa API/job failure. API p50 **119.59 ms**, p95 **898.06 ms**, request throughput **24.52/detik**, queue p95 **299.70 ms**, migration p95 **9839.70 ms**, dan completion throughput **0.655 job/detik**.

Analisis tiap fase:

- **Normal:** API p95 140.78 ms berada di bawah target 750 ms; 9 job selesai tanpa error. Ini baseline singkat untuk fixture dan mesin ini.
- **Load:** target total VU naik ke 12; API p95 meningkat ke 397.81 ms, request throughput turun dari 34.20 ke 30.00/detik, dan 12 job selesai. Latency meningkat tetapi p95 fase ini masih di bawah target.
- **Stress:** target naik ke 24 VU; API p95 mencapai 1081.84 ms, migration p95 10059.20 ms, dan completion throughput turun ke 0.45 job/detik. Sistem tetap menyelesaikan job tanpa error tercatat, tetapi target latency terlampaui. Metrik agregat ini belum mengisolasi penyebab pada CPU, database, hashing atau lock.
- **Recovery:** setelah target turun kembali ke 5 VU, median API turun dari 303.86 ke 115.67 ms dan completion throughput naik ke 0.64 job/detik. Sebanyak 22 job selesai, termasuk pekerjaan dari fase sebelumnya. API p95 916.13 ms dan migration p95 10042.70 ms masih lebih tinggi dari normal; durasi recovery ini belum membuktikan tail latency pulih penuh.

**Status threshold:** API p95 global **gagal** (`898.06 ms ≥ 750 ms`), sehingga k6 mengembalikan **exit 99**. API error rate, job failure rate dan job completed **lulus**. Ambang per fase yang longgar tidak mengubah kegagalan threshold global. Tidak ada threshold yang dilonggarkan untuk run ini. Durasi singkat dan penggunaan workstation bersama membatasi hasil sebagai estimasi kapasitas produksi.

### Security scanning dengan CodeQL

Scan baru memakai CodeQL CLI **2.27.4**, query pack Go **1.6.14**, JavaScript **2.5.0**, dan suite **security-extended**. Database analisis dibuat baru dari source saat ini; hasil lama tidak dipakai. [manifest.json](output/codeql/manifest.json) mencatat SHA-256 SARIF, source archive dan cakupan; [review.json](output/codeql/review.json) mencatat review setiap temuan. Semua source yang diarsipkan scanner telah dibandingkan dengan file saat ini dan cocok.

| Scan | Source bahasa yang diarsipkan | Temuan | Bukti |
| --- | ---: | ---: | --- |
| Go, Linux manual build | 138 file Go | 3 | [go.sarif](output/codeql/go.sarif) |
| JavaScript/TypeScript | 56 file JS/TS/MJS, serta 2 HTML entry point | 10 | [javascript.sarif](output/codeql/javascript.sarif) |

Go mencakup source aplikasi user/admin, shared backend, schemas dan tooling untuk build Linux. Dari 139 file Go di luar `src/tests/`, hanya `process_windows.go` tidak diekstrak karena build constraint Windows. Sebanyak 100 file Go di `src/tests/` tidak diekstrak oleh konfigurasi default ini meskipun build command mengompilasi test; keterbatasan itu dicatat eksplisit. JavaScript mencakup semua 56 source bahasa yang terlihat oleh Git, termasuk frontend, k6, browser test dan capture tool. Generated bundles, dependency vendor dan output bukti tidak menjadi source frontend yang dipindai. Keberhasilan scan berarti analisis selesai, bukan jaminan bebas kerentanan.

Berikut seluruh **13 temuan baru**, tanpa suppression. ID pada tabel sama dengan `display_id` di review JSON. Severity memakai `SARIF level / security-severity`; keputusan dan alasan di bawah berlaku untuk sink serta trust boundary source yang baru diperiksa.

| ID | Rule | Severity | Lokasi source | Dampak, keputusan dan alasan |
| --- | --- | --- | --- | --- |
| G01 | `go/sql-injection` | error / 8.8 | `src/apps/user/backend/migration/engines/postgres/execution.go:34:39` | SQL berizin dapat mengubah/menghapus data target. **Risiko diterima:** sink ini memang mengeksekusi migration; permission, immutable revision/hash, approval, validasi worker/parser dan protected metadata membatasi jalurnya. Role target tetap harus dibatasi. |
| G02 | `go/log-injection` | error / 6.1 | `src/apps/user/backend/migration/workflow.go:94:94` | Input dapat memalsukan log bila ditulis mentah. **False positive:** workflow default memakai `slog.NewJSONHandler`; field dinamis di-encode dan karakter kontrol di-escape. |
| G03 | `go/log-injection` | error / 6.1 | `src/shared/backend/observability/telemetry.go:227:40` | Input dapat memalsukan log teks. **False positive:** konfigurasi aplikasi memakai JSON dan OTLP structured fields dengan redaction sebelum fan-out; custom sink teks perlu review baru. |
| J01 | `js/bad-code-sanitization` | error / 6.1 | `src/tests/frontend/application-ui.mjs:701:120` | Interpolasi dapat mengeksekusi kode browser. **False positive:** nilai pencarian control di-encode sebagai literal utuh dengan `JSON.stringify` untuk CDP lokal. |
| J02 | `js/bad-code-sanitization` | error / 6.1 | `src/tests/frontend/application-ui.mjs:701:224` | Interpolasi dapat mengeksekusi kode browser. **False positive:** nilai pada pesan missing control memakai literal `JSON.stringify`, berasal dari source test. |
| J03 | `js/bad-code-sanitization` | error / 6.1 | `src/tests/frontend/application-ui.mjs:701:306` | Interpolasi dapat mengeksekusi kode browser. **False positive:** nilai pada pesan disabled control di-encode sebagai literal JavaScript, tidak dimasukkan ke HTML. |
| J04 | `js/bad-code-sanitization` | error / 6.1 | `src/tests/frontend/application-ui.mjs:705:126` | Interpolasi dapat mengeksekusi kode browser. **False positive:** label field memakai `JSON.stringify` pada helper CDP test lokal. |
| J05 | `js/bad-code-sanitization` | error / 6.1 | `src/tests/frontend/application-ui.mjs:705:335` | Interpolasi dapat mengeksekusi kode browser. **False positive:** nilai input field menjadi literal `JSON.stringify` pada `Runtime.evaluate`, bukan fragmen kode request publik. |
| J06 | `js/bad-code-sanitization` | error / 6.1 | `src/tests/frontend/application-ui.mjs:709:105` | Interpolasi dapat mengeksekusi kode browser. **False positive:** label checkbox berasal dari test dan di-encode menjadi literal utuh. |
| J07 | `js/file-access-to-http` | warning / 6.5 | `src/tests/frontend/application-ui.mjs:599:30` | Modifikasi file port dapat mengarahkan request lokal. **Risiko diterima:** file `DevToolsActivePort` berasal dari profile temporary private milik Chrome harness; host tetap `127.0.0.1`. |
| J08 | `js/file-access-to-http` | warning / 6.5 | `src/tools/capture-grafana.mjs:13:9` | Password file dikirim melalui HTTP. **Risiko diterima:** password disposable sengaja dikirim ke login Grafana pada URL loopback tetap; tidak dipakai di produksi atau dimuat ke proof. |
| J09 | `js/file-access-to-http` | warning / 6.5 | `src/tools/capture-grafana.mjs:33:38` | File port dapat memengaruhi request CDP. **Risiko diterima:** profile browser temporary private dan host loopback tetap; perubahan oleh proses lokal dengan akses user sama berada dalam trust boundary tool. |
| J10 | `js/http-to-file-access` | warning / 6.3 | `src/tests/frontend/application-ui.mjs:673:53` | Data HTTP dapat memengaruhi penulisan file. **False positive:** path diagnostik tetap `timeout.json`; hanya isi JSON berasal dari browser, dan file timeout tetap ignored. |

Review run ini memutuskan sembilan false positive dan empat risiko yang diterima pada batas penggunaan tersebut; tidak ada temuan baru yang diputuskan perlu perubahan source. Karena itu tidak ada commit perbaikan atau klaim before/after baru untuk run ini. Semua 13 alert tetap tampak pada SARIF, bersama alasan individual dalam review JSON. Perubahan input, logger, permission atau penggunaan tool sebagai service publik memerlukan review ulang.

SAST dapat dijalankan ulang melalui runner repo dengan `--stage codeql`. [Workflow template](src/tools/ci-templates/workflows/codeql.yml) mempunyai path konfigurasi yang sudah diperbarui, tetapi tetap **nonaktif** di luar `.github/workflows`; bukti ini adalah scan CLI lokal, bukan hosted GitHub Actions. Untuk otomatisasi push/PR, template perlu dipasang pada lokasi workflow GitHub. Tidak ada hasil hosted run yang diklaim.
