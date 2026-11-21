# SUKAMOTO

## Menjalankan aplikasi

Jalankan server Go dari direktori `app`:

```sh
cd app && /usr/local/go/bin/go run ./cmd/sukamoto
```

Secara default aplikasi berjalan di `http://localhost:8080`, menggunakan
`app/data/sukamoto.db` untuk SQLite dan `app/data/uploads/payment-proofs` untuk
bukti pembayaran. Konfigurasi dapat diubah dengan environment variable:

| Variable | Default | Kegunaan |
| --- | --- | --- |
| `SUKAMOTO_ADDR` | `:8080` | Alamat dan port HTTP |
| `DATABASE_PATH` | `data/sukamoto.db` | Lokasi database SQLite |
| `UPLOAD_DIR` | `data/uploads/payment-proofs` | Penyimpanan privat bukti pembayaran |
| `APP_ENV` | `development` | Gunakan `production` untuk cookie aman HTTPS |
| `STATIC_DIR` | `..` | Direktori root aset statis |

Database dimigrasikan otomatis saat aplikasi dimulai. CSS dapat dibangun ulang
dari direktori `app` menggunakan `npm run css:build`.

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
- Halaman pelanggan berbasis akun lama dinonaktifkan; data akun dan pesanan
  lama tetap dipertahankan oleh migrasi.

Editor data undangan modular, penegakan entitlement fitur per paket, dan
publikasi/render undangan belum tersedia. Status `published` tidak berarti
undangan publik dapat dibuat melalui aplikasi saat ini.
