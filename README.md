# Schakker

Schakker adalah aplikasi pengelolaan database dengan autentikasi pengguna, akses berbasis grup, penjelajah skema dan data, serta pengelolaan migrasi SQL. Database internal website menyimpan akun, sesi, permission, dan riwayat migrasi. Database target yang dikelola dapat menggunakan PostgreSQL atau MariaDB.

## 1. Dependensi yang perlu dipasang

Petunjuk berikut ditujukan untuk Linux, terutama Ubuntu/Debian. Untuk Windows, gunakan Ubuntu melalui WSL2 dan pasang dependensi di lingkungan Ubuntu tersebut.

| Dependensi | Kebutuhan | Sumber unduhan |
| --- | --- | --- |
| Go | Versi 1.26 atau lebih baru. Proyek mencantumkan toolchain `go1.26.10` di `src/go.mod`. | [Instalasi Go resmi](https://go.dev/doc/install) |
| PostgreSQL | Versi 14 atau lebih baru, termasuk program server `postgres`, `initdb`, dan `pg_ctl`. Diperlukan untuk database lokal dan integration test. | [Unduhan PostgreSQL](https://www.postgresql.org/download/) |
| PostgreSQL client | Menyediakan `psql` untuk pemeriksaan database bila diperlukan. | [Paket PostgreSQL Ubuntu](https://www.postgresql.org/download/linux/ubuntu/) |
| Bash | Versi 4 atau lebih baru untuk menjalankan `run.sh`. | Paket `bash` pada distribusi Linux. |
| Browser | Untuk membuka antarmuka website. | Browser yang tersedia di komputer Anda. |

MariaDB Server bersifat **opsional**, jika ingin mengelola target MariaDB atau menggunakan database mock MariaDB dari launcher. Adapter mendukung MariaDB 10.6 atau lebih baru; pilih rilis yang masih didukung melalui [unduhan resmi MariaDB](https://mariadb.org/download/). Mock MariaDB memerlukan `mariadbd` dan `mariadb-install-db`. Database internal website tetap menggunakan PostgreSQL.

### Instalasi di Ubuntu/Debian

Pasang PostgreSQL dan Bash:

```bash
sudo apt update
sudo apt install postgresql postgresql-client bash
```

Jika ingin menggunakan mock MariaDB, pasang juga server MariaDB:

```bash
sudo apt install mariadb-server
```

Launcher menyiapkan instance MariaDB privat dengan direktori data dan port sendiri. Tidak perlu membuat database, akun MariaDB, atau menjalankan service MariaDB sistem secara manual. Inisialisasi menggunakan [`mariadb-install-db`](https://mariadb.com/docs/server/clients-and-utilities/deployment-tools/mariadb-install-db).

Untuk Go, ikuti [petunjuk instalasi resmi](https://go.dev/doc/install), pilih paket Linux sesuai arsitektur komputer, dan pastikan perintah `go` tersedia pada `PATH`.

Periksa hasil instalasi:

```bash
go version
psql --version
bash --version
ls /usr/lib/postgresql/*/bin/{initdb,pg_ctl,postgres}
```

Pada instalasi PostgreSQL di luar lokasi standar, atur `SCHAKKER_POSTGRES_BIN` seperti dijelaskan pada bagian konfigurasi opsional.

Dependensi Go pada `src/go.mod` diunduh otomatis ketika proyek dibangun. Siapkan koneksi internet pada penggunaan pertama jika dependensi atau toolchain belum tersedia di cache.

## 2. Menjalankan website

Buka terminal di root repositori, yaitu folder yang berisi `README.md` dan `run.sh`, lalu jalankan:

```bash
./run.sh
```

Jalankan sebagai pengguna biasa. Launcher akan membangun aplikasi, menyiapkan database PostgreSQL privat untuk proyek, membuat tabel yang diperlukan, dan membuat serta menyimpan master key secara otomatis. Pengaturan bawaan dapat digunakan tanpa membuat file `.env`.

Jika muncul `Permission denied` saat membuka launcher, gunakan:

```bash
bash run.sh
```

### Pilihan saat launcher dijalankan

Launcher menampilkan lima pertanyaan berikut:

| Pertanyaan | Jawaban `y` | Jawaban `n` | Bawaan saat menekan Enter |
| --- | --- | --- | --- |
| Menjalankan unit test? | Menjalankan pengujian dengan dependensi database yang di-stub. | Melewati unit test. | `y` |
| Menjalankan integration test PostgreSQL? | Menjalankan integration test dengan database pengujian terisolasi; pengujian fixture MariaDB juga berjalan jika tools tersedia. | Melewati integration test. | `y` |
| Mempertahankan database mock website? | Menggunakan database contoh untuk akun, grup, sesi, dan riwayat website. | Menggunakan database website normal atau koneksi yang dikonfigurasi. | `n` |
| Mempertahankan database mock managed PostgreSQL? | Menyediakan target PostgreSQL contoh berisi tabel `employees` dengan data Alice dan Bob. | Menjalankan website tanpa target mock PostgreSQL tersebut. | `n` |
| Mempertahankan database mock MariaDB? | Menyediakan target MariaDB contoh berisi tabel `employees` dengan data Alice dan Bob. | Menjalankan website tanpa target mock MariaDB tersebut. | `n` |

Jika ada pengujian yang dipilih tetapi gagal, launcher menyimpan hasilnya dan menghentikan proses sebelum website dijalankan.

Database mock website, managed PostgreSQL, dan MariaDB dapat dipilih secara terpisah. Pilihan MariaDB dapat ditambahkan ke semua kombinasi berikut:

| Mock website | Mock managed | Hasil |
| --- | --- | --- |
| `n` | `n` | Menggunakan database website normal. Target database dapat didaftarkan melalui halaman admin. |
| `y` | `n` | Menggunakan database website contoh; target database dapat didaftarkan sendiri. |
| `n` | `y` | Menggunakan database website normal dengan tambahan target managed contoh. |
| `y` | `y` | Menggunakan database website contoh dan target managed contoh. |

Setiap integration test menggunakan salinan database tersendiri. Database mock yang dipertahankan untuk website berisi data awal, bukan perubahan dari skenario pengujian.

### Membuat admin dan masuk

Jika database website yang dipilih belum memiliki pengguna, launcher meminta:

1. Username admin; nilai bawaan `admin`.
2. Nama tampilan; nilai bawaan `Platform Admin`.
3. Password sepanjang **12–72 byte**, kemudian konfirmasinya. Password tidak ditampilkan saat diketik.

Setelah terminal menampilkan alamat server, buka:

**http://localhost:3000**

Masuk menggunakan akun admin yang baru dibuat. Pada database mock website baru, akun `mock.writer` dan `mock.reader` menggunakan password yang dipilih saat pembuatan admin pertama.

### Mengakses database target

Jika menggunakan target mock, launcher mendaftarkan koneksinya dan menyiapkan permission grup contoh. Jika mendaftarkan target sendiri:

1. Buka **Administration → DB Connections** dan simpan koneksi PostgreSQL atau MariaDB.
2. Buka **Groups**, buat grup, lalu tambahkan pengguna yang memerlukan akses, termasuk akun admin jika diperlukan.
3. Buka **Access Control** dan berikan permission `View Only` atau `View + Edit` untuk grup tersebut.
4. Buka **My Databases** untuk menjelajahi target atau membuat migrasi sesuai permission.

Peran Platform Admin dan permission database dikelola terpisah. Akses database diberikan melalui keanggotaan grup.

### Menghentikan dan menjalankan kembali

Tekan **Ctrl+C** pada terminal launcher untuk menghentikan website serta instance PostgreSQL dan MariaDB yang dimulainya. File database yang dipilih untuk dipertahankan tetap disimpan dan digunakan kembali pada run berikutnya.

Data lokal, master key, dan database mock yang dipertahankan disimpan di `src/.schakker/`. Pertahankan folder ini agar data dan kunci tetap konsisten antar sesi. Folder tersebut dikecualikan dari Git.

Untuk menggunakan kembali database mock yang sebelumnya dipertahankan, jalankan `./run.sh`, lewati integration test, lalu pilih `y` untuk database mock yang ingin digunakan kembali. Jika menjalankan integration test lagi dan memilih mempertahankan mock, launcher menggunakan kumpulan fixture baru dari pengujian tersebut.

## 3. Pengujian dan hasilnya

Launcher menyimpan hasil pengujian otomatis pada:

```text
src/test/output/<waktu-run>/
├── unit.log
├── integration.log
└── summary.txt
```

File log dibuat sesuai suite yang dipilih. `summary.txt` mencatat status lulus atau gagal. Laporan hasil pengujian dikecualikan dari Git.

Lokasi kode pengujian dan data contoh:

```text
src/test/
├── unit/                 # Unit test dengan framework testing bawaan Go
├── integration/          # Pengujian dengan database nyata
├── data/
│   ├── website/          # SQL data contoh akun, grup, dan keanggotaan
│   ├── managed/          # SQL tabel dan data target PostgreSQL contoh
│   └── mariadb/          # SQL tabel dan data target MariaDB contoh
└── output/               # Laporan pengujian dari launcher
```

Untuk menjalankan pengujian secara langsung dari terminal:

```bash
cd src
go test -count=1 ./test/unit/...
go test -count=1 -timeout=5m ./test/integration/...
```

Unit test dapat dijalankan tanpa server database. Integration test menyiapkan PostgreSQL privat dan database pengujian secara otomatis. Pengujian fixture MariaDB dilewati jika tools MariaDB belum dipasang; jika tersedia, pengujian membuat instance privat dan memeriksa data contoh, permission, serta penggunaan ulang database. Perintah `go test` langsung menampilkan hasil di terminal; gunakan launcher untuk menyimpan laporan secara otomatis ke `src/test/output/`.

## 4. Konfigurasi opsional

Untuk mengubah pengaturan bawaan, salin contoh konfigurasi dari root repositori:

```bash
cp src/.env.example src/.env
```

Edit `src/.env`, kemudian jalankan kembali `./run.sh`. Launcher memuat file ini secara otomatis.

| Variabel | Kegunaan |
| --- | --- |
| `PORT` | Port website; bawaan `3000`. |
| `SCHAKKER_STATE_DIR` | Lokasi penyimpanan data lokal; bawaan `.schakker`, relatif terhadap `src/`. |
| `SCHAKKER_POSTGRES_BIN` | Direktori berisi `postgres`, `initdb`, dan `pg_ctl` jika tidak terdeteksi otomatis. Contoh: `/usr/lib/postgresql/14/bin`. |
| `SCHAKKER_MARIADB_BIN` | Direktori instalasi tools MariaDB jika tidak terdeteksi otomatis. Contoh: `/usr/sbin` atau `/opt/mariadb/bin`. Launcher juga mencari installer pada direktori `bin`, `sbin`, dan `scripts` di instalasi yang sama. |
| `SCHAKKER_DATABASE_URL` | Koneksi PostgreSQL internal website yang sudah tersedia. Jika dikosongkan, aplikasi menyiapkan database lokal. Database dengan koneksi khusus harus sudah ada; skema dibuat otomatis jika kosong. |
| `SCHAKKER_MASTER_KEY` | Kunci 32 byte dalam format hex atau Base64. Jika dikosongkan pada mode pengembangan, aplikasi membuat dan menyimpannya otomatis. |
| `SCHAKKER_ADMIN_USERNAME` | Username admin pertama jika ingin melewati pertanyaan pembuatan akun. |
| `SCHAKKER_ADMIN_NAME` | Nama tampilan admin pertama. |
| `SCHAKKER_ADMIN_PASSWORD` | Password admin pertama jika diberikan melalui konfigurasi. Tidak mengubah password akun yang sudah ada. |

Contoh mengganti port untuk satu kali menjalankan aplikasi:

```bash
PORT=3001 ./run.sh
```

Jika `PORT` juga diisi di `src/.env`, nilai dari file tersebut akan dimuat oleh launcher. Untuk memakai contoh perintah di atas, biarkan `PORT` tidak ditentukan di file atau ubah langsung nilai pada file tersebut.
