# SUKAMOTO — Phase 1 Audit Report (Initial Repository State)

**Tanggal audit:** 20 November 2026
**Commit yang diaudit:** `6d8c4bf` ("frist commit")
**Prinsip:** Tidak ada kode yang diubah/dihapus sebelum audit selesai dan rencana perubahan disusun.

---

## 1. Existing Architecture

### Struktur repository saat ini

```
/workspace
├── README.md                  (1 baris: "# hercuellessht" — bukan dokumentasi proyek)
├── index.html                 (551 baris — "landing page", HTML hasil save-from-browser)
├── index/                     (aset statis untuk index.html)
│   ├── all.min.css            (Font Awesome 100KB, diduplikasi 4x di repo)
│   ├── css2                   (Google Fonts cache, diduplikasi 4x)
│   └── photo-*.jpg x3         (foto paket)
├── silver_files/
│   ├── silver.html            (443 baris — halaman katalog template Silver)
│   ├── form.html              (32 baris — form pendaftaran statis, tidak terhubung ke mana pun)
│   ├── photo-1520854221256... (satu-satunya thumbnail template)
│   ├── all.min.css / css2     (duplikat)
├── gold_files/gold.html       (431 baris — katalog Gold, KOSONG: "Belum ada template di paket ini.")
└── platinum_files/platinum.html (431 baris — katalog Platinum, KOSONG)
```

### Teknologi yang teridentifikasi

| Aspek | Kondisi aktual |
|---|---|
| Framework | **Tidak ada.** Murni HTML statis hasil simpan browser (`<!-- saved from url=http://0.0.0.0:9000/ -->`) |
| Backend / API | **Tidak ada** |
| Database | **Tidak ada** |
| Autentikasi | **Tidak ada** (hanya "PIN admin" client-side) |
| CSS | Inline `<style>` per halaman, hard-coded, tidak ada design system |
| JS | Inline script kecil (toggle menu, PIN prompt) |
| Font/Ikon | Font Awesome lokal + Google Fonts cache (Plus Jakarta Sans, Alex Brush) |

**Kesimpulan arsitektur:** Repository adalah *snapshot* UI dari server sementara (`0.0.0.0:9000`) yang pernah berjalan, bukan basis kode aplikasi. Ada bukti pernah ada backend (URL `/templates?package_id=1`, `/template-action?id=5`, `/admin`), tetapi kodenya tidak ikut tersimpan.

### Rekomendasi stack (untuk disepakati sebelum Phase 1 build)

Mempertahankan estetika yang sudah ada (mobile-first, dark + gold #fbbf24, Plus Jakarta Sans + Alex Brush) dan kebutuhan rendering dinamis berbasis slug:

- **Frontend:** Next.js (React) — SSR/ISR untuk halaman undangan publik berdasarkan slug, App Router, Tailwind CSS untuk design system.
- **Backend:** API route Next.js + validasi Zod; atau FastAPI bila tim lebih memilih Python.
- **Database:** PostgreSQL (relational integrity, FK, unique slug) + Prisma ORM.
- **Auth:** Session-based dengan role (customer/admin).
- **Storage:** Lokal `uploads/` (dev) → dapat dipindah ke S3-compatible; validasi tipe/ukuran file.

Alasan tidak "mempertahankan teknologi utama": tidak ada framework yang layak dipertahankan karena repo hanya berisi HTML statis. Membangun fondasi full-stack adalah satu-satunya cara memenuhi requirement (builder, payment, admin, publishing). Ini direkomendasikan sebagai keputusan teknis dengan alasan kuat, bukan penggantian tanpa perlu.

---

## 2. Existing Features (yang benar-benar berfungsi)

1. **Halaman katalog per paket (statis):** `silver.html`, `gold.html`, `platinum.html` menampilkan navbar, heading, grid template, footer.
2. **Komponen UI yang bisa dipakai ulang (nilai desain layak dipertahankan):**
   - Navbar + hamburger dropdown menu.
   - Card template (`tmpl-card`: gambar, badge diskon, harga, durasi, tombol "Pilih Template").
   - Paket grid 2 kolom di beranda (`packages-grid-2col`).
   - Footer brand (logo, tagline, sosial media IG/FB/WA, copyright).
   - Kartu bio/quote hero.
   - Design tokens implisit: bg `#09090b/#0c0c0e`, border `#27272a`, aksen emas `#fbbf24`, radius 8–12px, mobile max-width 480px — konsisten dengan arah "Japanese-inspired minimalism + luxury".
3. **Satu form HTML mentah** (`form.html`): input nama/email/telp diri & pasangan, tanpa aksi backend, tidak dirujuk halaman lain.

## 3. Missing Features (dibanding spesifikasi)

Seluruh sistem inti belum ada: autentikasi & role, database, template catalog dinamis, template preview, invitation builder 14 modul, package feature-entitlement, checkout & payment + verifikasi admin, dashboard customer, dashboard admin, publishing undangan dengan slug unik, RSVP, guestbook, amplop digital, music/video/gallery, SEO/social metadata, sharing WhatsApp.

## 4. Critical Bugs & Issues

| # | Severity | Issue | Lokasi |
|---|---|---|---|
| B1 | **Critical** | Admin "auth" berupa PIN `110202` hard-coded di client-side — siapa pun bisa membaca sumbernya. | `accessAdmin()` di semua HTML |
| B2 | **Critical** | Semua tautan navigasi mengarah ke `http://0.0.0.0:9000/...` (server mati) — navigasi antarhalaman rusak total kecuali beberapa link relatif. | navbar, back-link, brand, template-action |
| B3 | High | Path gambar rusak: `src="./SUKA MOTO_silver_files/photo-..."` padahal folder sebenarnya `silver_files/`. Thumbnail template tidak akan tampil. | `silver.html` baris ~360 |
| B4 | High | Aset image dirujuk tanpa ekstensi (`./index/photo-1519741497674-611481863552` vs file `.jpg`) — bergantung pada rewrite server lama. | `index.html` |
| B5 | Medium | Duplikasi berat: `all.min.css` & `css2` disalin 4x; seluruh CSS+JS halaman disalin-per-tempel antar HTML (~90% kode sama). | tiap folder `*_files` |
| B6 | Medium | Data template hardcoded ("Kwkwk", harga `100000`, durasi `30hari`, id=5) — tidak bersumber dari data. | `silver.html` |
| B7 | Low | `form.html` yatim: tidak ditautkan dari halaman mana pun, tidak punya action/validasi nyata. | `silver_files/` |
| B8 | Low | README bukan dokumentasi proyek; tidak ada `.gitignore`, `.env.example`, struktur sumber daya. | root |

## 5. UI/UX Improvements

- Pertahankan palet gelap-emas, tipografi (Plus Jakarta Sans + Alex Brush cursive untuk aksen), layout mobile-first 480px — sesuai arahan premium/Japanese-minimal.
- Tambah: design system token (spacing, radius, warna) agar tidak inline-style; hierarki CTA jelas ("Buat Undangan" vs "Lihat Template"); empty state yang informatif; indikator progress pengisian undangan; toast notifikasi "data tersimpan"; komponen form modular per modul builder.
- Perbaiki: kontras teks kecil (10–11px di kartu paket), tautan mati (B2), thumbnail hilang (B3).

## 6. Database Improvements (skema awal yang direkomendasikan)

Entitas sesuai spesifikasi §12, disederhanakan ke relasi inti: `users(role)`, `packages`, `package_features(feature_key, limits)`, `templates(package_id, category, config_json, is_active)`, `orders(user_id, package_id, template_id, status)`, `payments(order_id, proof_path, status[pending/submitted/verified/rejected])`, `invitations(order_id, slug UNIQUE, published_at, expires_at, is_active)`, tabel section per modul (`invitation_events`, `_photos`, `_videos`, `_music`, `_rsvps`, `_guests`, `_messages`, `_gifts`) — bukan satu tabel besar JSON. Index pada `slug`, `order status`, FK penuh, constraint unique pada slug.

## 7. Recommended Development Phases (berdasarkan kondisi aktual)

Karena repo hanya berisi HTML statis, Phase 1 mencakup setup fondasi penuh:

- **Phase 1 — Foundation:** Scaffold Next.js + Prisma + Postgres; design system dari token yang ada; skema DB + seed paket (Silver/Gold/Platinum-ready); auth registrasi/login + role; public landing page fungsional (navbar Home/Templates/Pricing/How-it-works/Login/Create Invitation, hero, featured templates dari DB, pricing 3 paket, advantages, footer). Hapus PIN client-side.
- **Phase 2 — Template System:** Katalog dinamis (filter paket/kategori, search), preview, konfigurasi template (layout/warna/font), pemilihan template.
- **Phase 3 — Customer System:** Dashboard + builder multi-step 14 modul dengan autosave per modul; feature-entitlement diperiksa di backend.
- **Phase 4 — Payment:** Checkout, kode transaksi, form pembayaran + upload bukti, status 4 tahap, notifikasi admin.
- **Phase 5 — Admin:** Kelola pelanggan, pesanan, verifikasi pembayaran, aktivasi/nonaktifkan undangan, kelola template & paket.
- **Phase 6 — Publication:** Rendering dinamis via slug, RSVP & guestbook persisten, WhatsApp share, SEO/OG metadata.
- **Phase 7 — Testing & Optimization:** Suite uji untuk 15 skenario §15, uji keamanan akses lintas-paket, responsif, performa.

**Aturan transisi:** File HTML lama tetap disimpan di repo (referensi desain) sampai halaman pengganti lolos testing; tidak ada fitur yang dihapus tanpa pengganti.
