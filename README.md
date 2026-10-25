# Schakker

## Lingkungan yang disarankan

**Disarankan menggunakan Linux (Ubuntu atau Debian)** dengan Bash. Proyek juga dapat dijalankan di Windows 10/11 menggunakan Windows PowerShell 5.1 atau lebih baru. Jika menggunakan WSL2, ikuti langkah Linux dan pasang dependensi di dalam WSL2.

Siapkan koneksi internet untuk mengunduh dependensi Go dan toolchain saat build pertama. Port bawaan aplikasi adalah `3000`; pastikan port tersebut tersedia.

## Dependensi dan unduhan

| Perangkat lunak | Kebutuhan | Unduhan / instalasi |
| --- | --- | --- |
| Go | Wajib, versi 1.26 atau lebih baru. `src/go.mod` menetapkan toolchain `go1.26.10`. | [Unduh dan pasang Go](https://go.dev/doc/install) sesuai sistem operasi dan arsitektur komputer. |
| PostgreSQL Server | Wajib untuk menjalankan database lokal bawaan. Pasang versi 14 atau lebih baru, termasuk `postgres`, `initdb`, dan `pg_ctl`. | [Unduh PostgreSQL](https://www.postgresql.org/download/). |
| Bash | Wajib untuk launcher Linux, versi 4 atau lebih baru. | Tersedia melalui paket `bash` pada Linux. |
| Windows PowerShell | Wajib untuk launcher Windows, versi 5.1 atau lebih baru. | Sudah tersedia pada Windows 10/11. |
| Browser | Wajib untuk membuka aplikasi. | Gunakan browser yang tersedia di komputer. |
| MariaDB Server | Opsional, jika ingin menjalankan database contoh (mock) MariaDB secara lokal. Target MariaDB yang didukung minimal versi 10.6. | [Unduh MariaDB](https://mariadb.org/download/) dan pilih rilis yang masih didukung. |
| Git | Opsional, untuk mengunduh repositori melalui `git clone`. | Dapat digantikan dengan mengunduh dan mengekstrak ZIP repositori. |

Dependensi Go utama berikut diunduh otomatis saat build, atau melalui `go mod download` dari folder `src/`:

| Modul | Versi |
| --- | --- |
| `github.com/go-sql-driver/mysql` | `v1.9.3` |
| `github.com/jackc/pgx/v5` | `v5.11.0` |
| `golang.org/x/crypto` | `v0.58.0` |

Dependensi transitif mengikuti `src/go.mod` dan `src/go.sum` serta diunduh otomatis bersama modul di atas.

## Build dan menjalankan di Linux

### 1. Pasang dependensi

Untuk Ubuntu/Debian:

```bash
sudo apt update
sudo apt install postgresql postgresql-client bash
```

Pasang Go sesuai [panduan instalasi resmi untuk Linux](https://go.dev/doc/install). Pastikan direktori Go sudah masuk ke `PATH`, lalu periksa instalasi:

```bash
go version
psql --version
bash --version
ls /usr/lib/postgresql/*/bin/{postgres,initdb,pg_ctl}
```

Jika ingin menggunakan mock MariaDB, pasang juga:

```bash
sudo apt install mariadb-server
```

Mock MariaDB memerlukan `mariadbd` dan `mariadb-install-db`.

### 2. Build dan jalankan aplikasi

Unduh atau clone repositori, lalu buka terminal di folder utama yang berisi `README.md` dan `run.sh`:

```bash
bash run.sh
```

Launcher akan membangun aplikasi, menyiapkan database PostgreSQL lokal, dan menjalankan server. Jalankan sebagai pengguna biasa.

Jika hanya ingin melakukan build tanpa menjalankan server:

```bash
cd src
go mod download
go build -o schakker .
cd ..
```

Perintah tersebut menghasilkan executable `src/schakker`. Gunakan `bash run.sh` untuk menjalankan aplikasi dengan penyiapan awal melalui launcher.

## Build dan menjalankan di Windows

### 1. Pasang dependensi

1. Pasang Go menggunakan installer Windows dari [halaman instalasi Go](https://go.dev/doc/install).
2. Pasang PostgreSQL Server dari [halaman unduhan PostgreSQL](https://www.postgresql.org/download/) dengan memilih Windows. Sertakan `postgres.exe`, `initdb.exe`, dan `pg_ctl.exe`. Direktori standar `C:\Program Files\PostgreSQL\<versi>\bin` dideteksi otomatis.
3. Jika ingin menggunakan mock MariaDB, pasang MariaDB Server dari [halaman unduhan MariaDB](https://mariadb.org/download/). Pastikan tersedia `mariadbd.exe` dan `mariadb-install-db.exe`; nama lama `mysqld.exe` dan `mysql_install_db.exe` juga didukung.
4. Buka kembali PowerShell setelah instalasi, lalu periksa Go dan versi PowerShell:

```powershell
go version
$PSVersionTable.PSVersion
```

### 2. Build dan jalankan aplikasi

Unduh atau clone repositori, lalu buka PowerShell di folder utama yang berisi `README.md` dan `run.bat`:

```powershell
.\run.bat
```

Launcher akan membangun aplikasi, menyiapkan database PostgreSQL lokal, dan menjalankan server. Jalankan sebagai pengguna biasa. Database proyek dan service Windows tidak perlu dibuat secara manual.

Jika hanya ingin melakukan build tanpa menjalankan server:

```powershell
cd src
go mod download
go build -o schakker.exe .
cd ..
```

Perintah tersebut menghasilkan executable `src\schakker.exe`. Gunakan `.\run.bat` untuk menjalankan aplikasi dengan penyiapan awal melalui launcher.

## Penyiapan saat menjalankan aplikasi

Kedua launcher menampilkan pilihan berikut. Jawab `y` untuk ya atau `n` untuk tidak; tekan Enter untuk menggunakan nilai bawaan.

| Pilihan | Nilai bawaan |
| --- | --- |
| Jalankan unit test sebelum aplikasi dimulai | `y` |
| Jalankan integration test PostgreSQL | `y` |
| Gunakan database mock website | `n` |
| Gunakan database mock PostgreSQL yang dikelola | `n` |
| Gunakan database mock MariaDB | `n` |

Untuk menjalankan tanpa pengujian dan data contoh, jawab `n` pada kelima pilihan. Pilih `y` untuk mock MariaDB hanya jika tools MariaDB sudah dipasang. Jika pengujian yang dipilih gagal, launcher menghentikan proses; laporan tersedia di `src/test/output/`.

Pada startup pertama, jika belum ada pengguna, masukkan username admin (bawaan `admin`), nama tampilan (bawaan `Platform Admin`), serta password sepanjang **12–72 byte** dan konfirmasinya.

Setelah terminal menampilkan alamat server, buka **http://localhost:3000** di browser dan masuk menggunakan akun admin tersebut. Tekan **Ctrl+C** di terminal untuk menghentikan aplikasi dan instance database yang dijalankannya.

Jalankan kembali launcher yang sama untuk menggunakan aplikasi pada sesi berikutnya. Data lokal disimpan di `src/.schakker/` pada Linux, atau `%LOCALAPPDATA%\Schakker\<id-proyek>\` pada Windows. Lokasi yang digunakan ditampilkan saat launcher berjalan. Pertahankan folder data beserta key di dalamnya, dan gunakan folder data terpisah untuk Linux dan Windows.

## Konfigurasi opsional

Pengaturan bawaan dapat digunakan tanpa file `.env`. Jika diperlukan, buat `src/.env` berisi satu penugasan `NAMA=nilai` per baris, lalu jalankan kembali launcher.

| Variabel | Kegunaan |
| --- | --- |
| `PORT` | Mengganti port aplikasi, misalnya `PORT=3001`. |
| `SCHAKKER_POSTGRES_BIN` | Direktori berisi `postgres`, `initdb`, dan `pg_ctl` jika tidak terdeteksi otomatis. Gunakan nama berakhiran `.exe` pada Windows. |
| `SCHAKKER_MARIADB_BIN` | Direktori tools MariaDB jika tidak terdeteksi otomatis. |
| `SCHAKKER_STATE_DIR` | Mengganti lokasi penyimpanan data lokal. Path relatif dihitung dari folder `src/`. |

Contoh `src/.env` di Linux; sesuaikan versi PostgreSQL dengan instalasi:

```dotenv
PORT=3000
SCHAKKER_POSTGRES_BIN=/usr/lib/postgresql/18/bin
```

Contoh `src/.env` di Windows; sesuaikan versi dan lokasi instalasi:

```dotenv
PORT=3000
SCHAKKER_POSTGRES_BIN="C:\Program Files\PostgreSQL\18\bin"
```
