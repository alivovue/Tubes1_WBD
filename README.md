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

## Instalasi dependensi di Linux

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

## Instalasi dependensi di Windows

1. Pasang Go menggunakan installer Windows dari [halaman instalasi Go](https://go.dev/doc/install).
2. Pasang PostgreSQL Server dari [halaman unduhan PostgreSQL](https://www.postgresql.org/download/) dengan memilih Windows. Sertakan `postgres.exe`, `initdb.exe`, dan `pg_ctl.exe`. Direktori standar `C:\Program Files\PostgreSQL\<versi>\bin` dideteksi otomatis.
3. Jika ingin menggunakan mock MariaDB, pasang MariaDB Server dari [halaman unduhan MariaDB](https://mariadb.org/download/). Pastikan tersedia `mariadbd.exe` dan `mariadb-install-db.exe`; nama lama `mysqld.exe` dan `mysql_install_db.exe` juga didukung.
4. Buka kembali PowerShell setelah instalasi, lalu periksa Go dan versi PowerShell:

```powershell
go version
$PSVersionTable.PSVersion
```

## Perintah build dan menjalankan aplikasi

Unduh atau clone repositori, lalu buka terminal di folder utama `Tubes1_WBD` yang berisi `README.md`, `run.sh`, dan `run.bat`. Jalankan perintah sesuai sistem operasi sebagai pengguna biasa.

### Jika menggunakan Linux

Untuk **build sekaligus menjalankan aplikasi**, gunakan Bash:

```bash
bash ./run.sh
```

Jika hanya ingin build tanpa menjalankan aplikasi:

```bash
cd src
go mod download
go build -o schakker .
cd ..
```

Hasil build saja adalah `src/schakker`. Untuk menjalankan aplikasi beserta penyiapan admin, gunakan `bash ./run.sh`.

### Jika menggunakan Windows

Untuk **build sekaligus menjalankan aplikasi**, gunakan PowerShell:

```powershell
.\run.bat
```

Jika menggunakan Command Prompt (CMD), perintahnya:

```bat
run.bat
```

Jika hanya ingin build tanpa menjalankan aplikasi, gunakan PowerShell atau CMD:

```powershell
cd src
go mod download
go build -o schakker.exe .
cd ..
```

Hasil build saja adalah `src\schakker.exe`. Untuk menjalankan aplikasi beserta penyiapan admin, gunakan `.\run.bat` di PowerShell atau `run.bat` di CMD.

Kedua launcher (`run.sh` dan `run.bat`) sudah mencakup build, penyiapan database PostgreSQL lokal, dan menjalankan server. Database proyek tidak perlu dibuat secara manual.

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

Untuk restart biasa, tekan **Ctrl+C**, lalu jalankan kembali `bash ./run.sh` (Linux), `.\run.bat` (Windows PowerShell), atau `run.bat` (Windows CMD). Akun dan data tetap tersimpan, sehingga penyiapan admin tidak diminta lagi.

## Reset penuh agar penyiapan admin muncul kembali

**Reset menghapus seluruh isi folder data lokal proyek**, termasuk akun admin, pengguna lain, sesi, pengaturan, database lokal, database mock yang disimpan, dan key. Database target pada server lain tidak ikut terhapus.

1. Hentikan aplikasi dengan **Ctrl+C** dan tunggu sampai proses berhenti.
2. Jika ada di `src/.env`, hapus baris `SCHAKKER_ADMIN_USERNAME`, `SCHAKKER_ADMIN_NAME`, dan `SCHAKKER_ADMIN_PASSWORD` agar akun admin tidak dibuat otomatis.
3. Panduan ini menggunakan database lokal bawaan. Jika sebelumnya memakai database eksternal, kosongkan `SCHAKKER_DATABASE_URL` dan `DATABASE_URL` di `src/.env` serta environment terminal sebelum melanjutkan. Gunakan mode pengembangan (hapus `SCHAKKER_ENV=production` jika sebelumnya diatur).
4. Jalankan perintah reset sesuai sistem operasi dari folder utama repositori.

### Jika menggunakan Linux

Untuk lokasi data bawaan (`src/.schakker/`):

```bash
unset SCHAKKER_ADMIN_USERNAME SCHAKKER_ADMIN_NAME SCHAKKER_ADMIN_PASSWORD
rm -rf -- ./src/.schakker
bash ./run.sh
```

Jika memakai `SCHAKKER_STATE_DIR`, ganti `./src/.schakker` pada perintah penghapusan dengan folder data yang dikonfigurasi. Path relatif pada variabel tersebut dihitung dari `src/`.

Pada WSL2, jika repositori berada di `/mnt/<drive>/...` dan `SCHAKKER_STATE_DIR` tidak diatur, lokasi data bawaan berada di `${XDG_STATE_HOME:-$HOME/.local/state}/schakker/<id-proyek>/`. Gunakan path lengkap dari baris **`WSL detected; PostgreSQL state:`** yang ditampilkan launcher sebagai pengganti `./src/.schakker`.

### Jika menggunakan Windows (PowerShell)

Data bawaan disimpan di `%LOCALAPPDATA%\Schakker\<id-proyek>\`. Salin **path lengkap** dari baris **`Schakker state:`** yang ditampilkan saat menjalankan `run.bat`. Jika memakai `SCHAKKER_STATE_DIR`, baris tersebut menampilkan lokasi khusus yang digunakan.

Jalankan perintah berikut melalui PowerShell. Saat diminta, tempel hanya path folder data proyek dari baris tersebut:

```powershell
$schakkerState = Read-Host 'Tempel path folder data dari baris Schakker state'
if ([string]::IsNullOrWhiteSpace($schakkerState)) { throw 'Path folder data harus diisi.' }
Remove-Item -LiteralPath $schakkerState -Recurse -Force -ErrorAction Stop
$env:SCHAKKER_ADMIN_USERNAME = $null
$env:SCHAKKER_ADMIN_NAME = $null
$env:SCHAKKER_ADMIN_PASSWORD = $null
.\run.bat
```

Setelah reset, jawab pilihan pengujian dan mock sesuai kebutuhan. Launcher akan meminta kembali **username admin, nama tampilan, password, dan konfirmasi password**. Untuk mulai dengan database kosong tanpa data contoh, jawab `n` pada ketiga pilihan mock.

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

## Kebijakan sesi

| Pengaturan | Nilai | Perilaku |
| --- | --- | --- |
| Idle timeout | 30 menit | Sesi berakhir jika tidak ada akses ke fitur yang dilindungi selama 30 menit. Akses berikutnya memperbarui batas idle, maksimal sampai absolute expiration. |
| Absolute expiration | 8 jam sejak login | Pengguna wajib login kembali setelah 8 jam, meskipun masih aktif menggunakan aplikasi. |
| Interval rotasi identifier | 15 menit | Identifier sesi diperbarui pada request terautentikasi berikutnya setelah interval tercapai. Identifier juga diperbarui ketika privilege meningkat; setiap login berhasil menghasilkan sesi baru. |
| Grace period | 30 detik | Setelah rotasi berkala atau peningkatan privilege, identifier lama tetap diterima selama 30 detik untuk menangani request bersamaan atau respons yang tidak diterima. Masa ini dibatasi oleh absolute expiration. |

Rotasi identifier tidak memperpanjang absolute expiration. Logout, penonaktifan akun, perubahan password, dan pencabutan akses melalui grup membatalkan sesi terkait di backend.

Mengubah pengaturan atau menonaktifkan koneksi database membatalkan sesi pengguna yang memiliki akses ke database tersebut, termasuk sesi admin jika admin juga memiliki akses melalui grup. Pengguna terkait harus login kembali; koneksi nonaktif tetap tidak dapat diakses sampai diaktifkan kembali.

Client menerima identifier sesi melalui cookie `schakker_session`; data sesi disimpan di backend. Cookie menggunakan `HttpOnly` dan `SameSite=Lax`, serta `Secure` pada HTTPS atau mode production. Request yang mengubah state dilindungi token CSRF.
