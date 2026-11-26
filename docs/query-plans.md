# Query Plan

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
