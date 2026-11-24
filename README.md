# SUKAMOTO

## Menjalankan aplikasi

Jalankan server dari root repository:

```sh
./run-server.sh
```

Skrip mencari Go di `PATH`, lalu mencoba `/usr/local/go/bin/go`. Untuk lokasi
Go lain, atur `GO_BIN`, misalnya `GO_BIN=/opt/go/bin/go ./run-server.sh`.

Secara default aplikasi berjalan di `http://localhost:8080`, menggunakan
`app/data/sukamoto.db` untuk SQLite dan `app/data/uploads/payment-proofs` untuk
bukti pembayaran. Konfigurasi dapat diubah dengan environment variable:

| Variable | Default | Kegunaan |
| --- | --- | --- |
| `SUKAMOTO_ADDR` | `:8080` | Alamat dan port HTTP |
| `DATABASE_PATH` | `data/sukamoto.db` | Lokasi database SQLite |
| `UPLOAD_DIR` | `data/uploads/payment-proofs` | Root penyimpanan privat bukti pembayaran, media undangan, dan library musik admin |
| `APP_ENV` | `development` | Gunakan `production` untuk cookie aman HTTPS |
| `STATIC_DIR` | `..` | Direktori root aset statis |

Database dimigrasikan otomatis saat aplikasi dimulai. Tailwind hanya dipakai
untuk membangun CSS saat pengembangan (Node.js 20 dan npm); production cukup
menjalankan binary Go dan menyajikan `app.css`, tanpa Node.js/npm runtime.
Bangun CSS dari direktori `app` menggunakan `npm run css:build`.

Tracking menggunakan kode baru 60-bit; kode 40-bit yang sudah diterbitkan tetap
diterima. Aplikasi memakai alamat peer TCP sebagai identitas rate limit dan
sengaja tidak mempercayai `X-Forwarded-For`. Jika dipasang di belakang reverse
proxy, batasi akses langsung ke aplikasi dan atur rate limit tracking di proxy
tepercaya.

### Membuat akun admin

Dari direktori `app`, buat admin lewat CLI. Nama menjadi nama tampilan dan email
menjadi ID untuk login di `/admin/login`. Password dibaca dari environment
variable dan tidak disimpan sebagai teks biasa:

```sh
SUKAMOTO_ADMIN_PASSWORD='password-kuat' /usr/local/go/bin/go run ./cmd/sukamoto-db create-admin data/sukamoto.db 'Nama Admin' admin@example.com
```

## Alur yang tersedia

- Tamu dapat memilih template dan paket, membuat pesanan, mengunggah bukti
  pembayaran, serta melacak status menggunakan kode pelacakan.
- Admin dapat masuk melalui `/admin/login`, meninjau bukti pembayaran, dan
  memperbarui status pesanan.
- Pemilik pesanan dapat membuka `/orders/{nomor-pesanan}/builder` pada perangkat
  yang menyimpan cookie akses pesanan untuk menyunting 13 modul draft: informasi
  dasar, sampul, kutipan/doa, kisah pasangan, acara/lokasi, galeri, video, musik,
  RSVP, buku tamu, amplop digital, ucapan tamu, dan tampilan/tautan.
- Kisah, acara, galeri, dan video mendukung penambahan, penghapusan, dan
  pengurutan item; batas foto, video, dan acara diperiksa kembali di backend
  menggunakan entitlement pesanan. Draft disimpan otomatis setelah jeda singkat
  dan tetap menyediakan tombol simpan manual.
- Penyimpanan modul dan perubahan metadata media memakai versi draft agar
  perubahan dari tab/perangkat lain tidak saling menimpa. Konflik autosave
  menghentikan penyimpanan otomatis dan mempertahankan perubahan lokal di sesi
  browser untuk pemulihan setelah memuat versi terbaru.
- Media menampilkan pratinjau privat, pilihan untuk sampul/kisah/galeri, caption,
  dan urutan. Upload dibatasi hingga 5 MiB, dialirkan ke file sementara dengan
  permission privat, lalu divalidasi berdasarkan signature. File media yatim
  berusia lebih dari 24 jam dibersihkan saat server dimulai; file yang masih
  tercatat di database tidak dihapus.
- Kode tracking tetap hanya untuk membaca status dan tidak dapat digunakan
  untuk mengedit draft.
- Admin dapat meninjau draft di detail pesanan, mengatur status review, dan
  mengelola pilihan musik di `/admin/music`. Admin harus memastikan hak
  penggunaan audio sebelum mengunggahnya.
- Halaman pelanggan berbasis akun lama dinonaktifkan; data akun dan pesanan
  lama tetap dipertahankan oleh migrasi.

Pratinjau undangan bersifat privat dan menampilkan draft yang sudah tersimpan.
Builder belum menerbitkan undangan publik; endpoint publik, RSVP yang
menyimpan jawaban, dan buku tamu publik belum tersedia. Status review `Siap
ditinjau` tidak berarti undangan telah dipublikasikan.
