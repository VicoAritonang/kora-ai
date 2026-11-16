# TECHNICAL_SPEC.md — Spesifikasi Teknis

Dokumen 1 dari 10 · Draft v1 · 14 November 2026

Dokumen terkait: [SYSTEM_ARCHITECTURE](SYSTEM_ARCHITECTURE.md) · [TECH_STACK](TECH_STACK.md) · [DATABASE_SCHEMA](DATABASE_SCHEMA.md) · [API_SPEC](API_SPEC.md) · [AI_SYSTEM_DESIGN](AI_SYSTEM_DESIGN.md) · [MODULE_SPEC](MODULE_SPEC.md) · [PROJECT_STRUCTURE](PROJECT_STRUCTURE.md) · [IMPLEMENTATION_PLAN](IMPLEMENTATION_PLAN.md) · [ENGINEERING_STANDARDS](ENGINEERING_STANDARDS.md). Riset pasar dan supplier ada di [PLAN.md](PLAN.md).

---

## 1. Tujuan teknis

Membangun agen AI travel agent yang menjawab dua pertanyaan:

1. **Tujuan → budget:** "Saya mau ke X, berapa biayanya?"
2. **Budget → tujuan:** "Saya punya Rp Y, bisa ke mana?"

Keluaran utama adalah 2–3 **paket perjalanan** dengan rincian biaya per item (penerbangan, hotel, aktivitas, makan, transport lokal, visa).

Tujuan teknis yang mengikat seluruh desain:

| ID | Tujuan |
|---|---|
| G1 | Tidak ada angka harga yang dihasilkan LLM. Semua harga berasal dari supplier atau knowledge graph, dan semua penjumlahan dilakukan kode deterministik. |
| G2 | Setiap harga membawa asal-usul: `live` (API, dengan waktu ambil dan kedaluwarsa) atau `estimate` (KG/rata-rata, dengan sumber dan tanggal). |
| G3 | Persona dan memori (OpenPersona), guardrails (guard-rails-engine), dan AI Lab terpasang lewat port, sehingga perubahan pada library pra-1.0 itu hanya menyentuh adapter. |
| G4 | Setiap langkah agen terekam sebagai event bertipe yang bisa dibaca AI Lab. |
| G5 | Model AI dan supplier bisa diganti lewat konfigurasi/adapter tanpa mengubah logika inti. |

## 2. Asumsi yang dipakai (menunggu keputusan CEO)

| Keputusan | Asumsi dokumen ini |
|---|---|
| Model bisnis MVP | Afiliasi/deep link. Tidak ada pembayaran di dalam aplikasi. |
| Cakupan destinasi awal | Jepang (Osaka, Kyoto, Tokyo), berangkat dari Jakarta, Surabaya, Denpasar. |
| Kanal | Web. WhatsApp menyusul. |
| Frontend | Next.js. Bisa diganti SvelteKit tanpa memengaruhi backend. |
| Bahasa produk | Indonesia sebagai utama, Inggris didukung. |

Bila salah satu berubah, bagian yang terdampak: FR-9 (booking), `DATABASE_SCHEMA` (tabel order), `API_SPEC` (endpoint pembayaran), dan Fase 3 di `IMPLEMENTATION_PLAN`.

## 3. Functional requirements

### 3.1 Percakapan dan pemahaman

| ID | Requirement | Prioritas |
|---|---|---|
| FR-1 | Pengguna mengirim permintaan dalam bahasa bebas (Indonesia/Inggris) dan menerima jawaban streaming. | MVP |
| FR-2 | Sistem mengekstrak slot perjalanan: asal, tujuan, budget + mata uang, tanggal atau musim, durasi, jumlah orang, minat, permintaan khusus. | MVP |
| FR-3 | Sistem bertanya balik **maksimal satu putaran** bila slot wajib kosong. Slot wajib: budget **atau** tujuan. Slot lain diisi asumsi yang ditampilkan ke pengguna dan bisa dikoreksi. | MVP |
| FR-4 | Pengguna bisa memakai sistem tanpa login (tamu). Login menyimpan riwayat dan memori. | MVP |
| FR-5 | Permintaan di luar cakupan (bukan perjalanan, atau destinasi yang belum didukung) ditolak dengan sopan dan jelas. | MVP |

### 3.2 Perencanaan dan paket

| ID | Requirement | Prioritas |
|---|---|---|
| FR-6 | Mode **tujuan → budget**: menghasilkan 2–3 paket (Hemat, Seimbang, Nyaman) untuk tujuan yang diminta. | MVP |
| FR-7 | Setiap paket memuat: penerbangan PP, penginapan, rencana harian, estimasi makan, transport lokal, aktivitas, biaya visa bila berlaku, total, dan sisa budget. | MVP |
| FR-8 | Setiap item paket menampilkan label `live` atau `estimasi`, sumber, dan waktu ambil. | MVP |
| FR-9 | Item `live` memiliki tautan keluar (deep link) ke penjual. Klik tercatat. | MVP |
| FR-10 | Bila budget tidak cukup, sistem menyatakannya terang-terangan dan menawarkan alternatif terurut: geser tanggal, kurangi malam, ganti area menginap, ganti destinasi, atau kekurangan dana dalam rupiah. | MVP |
| FR-11 | Pengguna bisa meminta perubahan ("ganti hotel lebih dekat stasiun", "tambah satu hari") dan sistem menyusun ulang paket. | MVP |
| FR-12 | Pengguna bisa mengecek ulang harga paket; sistem menampilkan selisih terhadap harga sebelumnya. | MVP |
| FR-13 | Mode **budget → tujuan**: dari budget saja, sistem menampilkan daftar destinasi yang muat beserta perkiraan total, dijawab dari Indeks Biaya Destinasi. | Fase 2 |
| FR-14 | Bila tanggal di luar jendela penjualan supplier, sistem memberi estimasi berlabel, menjelaskan kapan harga live tersedia, dan menawarkan pengingat. | Fase 2 |
| FR-15 | Permintaan khusus (misalnya festival musim gugur) dipenuhi dari KG persona; bila data tahun berjalan tidak ada, sistem melakukan riset web dan menandai hasilnya dengan sumber. | MVP |

### 3.3 Pengetahuan dan memori

| ID | Requirement | Prioritas |
|---|---|---|
| FR-16 | Retrieval KG persona berjalan di pipeline (sebelum loop agen) dan di dalam loop (lewat tool). Tidak ada jalur akses KG lain. | MVP |
| FR-17 | Konteks sesi dikelola berlapis: state terstruktur, jendela percakapan, ringkasan, artefak di luar konteks. | MVP |
| FR-18 | Memori lintas sesi (KG user) ditulis di akhir sesi dengan persetujuan pengguna; pengguna bisa melihat dan menghapusnya. | Fase 2 |

### 3.4 Pengawasan

| ID | Requirement | Prioritas |
|---|---|---|
| FR-19 | Guardrails (guard-rails-engine) dipanggil di empat titik: masuk, sebelum tool, sesudah tool, keluar. | MVP; penyetelan kebijakan di Fase 2 |
| FR-20 | Setiap run memancarkan event per langkah (model, token, biaya, argumen/hasil tool, latensi, putusan guardrail) ke AI Lab. | MVP |
| FR-21 | Pengguna bisa memberi penilaian pada jawaban dan paket. | MVP |

## 4. Non-functional requirements

### 4.1 Kinerja

| ID | Metrik | Target |
|---|---|---|
| NFR-1 | Waktu ke event status pertama | < 1 detik |
| NFR-2 | Waktu ke paragraf narasi pertama (tanpa pencarian supplier) | < 3 detik |
| NFR-3 | Waktu sampai paket lengkap, p50 / p95 | < 25 detik / < 45 detik |
| NFR-4 | Latensi endpoint non-AI, p95 | < 200 ms |

### 4.2 Kebenaran

| ID | Metrik | Target |
|---|---|---|
| NFR-5 | Paket yang lolos seluruh invarian kode sebelum ditampilkan | 100% (yang gagal tidak ditampilkan) |
| NFR-6 | Selisih total item `live` terhadap harga saat dicek ulang dalam masa berlaku | Dalam ±10% |
| NFR-7 | Kepatuhan batasan pengguna (budget, tanggal, permintaan khusus) di golden set | ≥ 95% |
| NFR-8 | Klaim faktual di narasi yang punya sumber | ≥ 95% |

### 4.3 Biaya

| ID | Metrik | Target |
|---|---|---|
| NFR-9 | Biaya LLM per run perencanaan | < $0,15 |
| NFR-10 | Pencarian supplier live per run | ≤ 6 penerbangan, ≤ 6 penginapan |
| NFR-11 | Pencarian web per run | ≤ 3 |

### 4.4 Keandalan dan operasi

| ID | Requirement |
|---|---|
| NFR-12 | Kegagalan satu supplier tidak menggagalkan run; paket tetap tersusun dengan item terdampak berlabel `estimate` dan catatan. |
| NFR-13 | Stream SSE bisa disambung ulang dengan `Last-Event-ID` tanpa kehilangan event. |
| NFR-14 | Run yang terputus karena restart server ditandai `failed` dan bisa diulang oleh pengguna; tidak ada run menggantung. |
| NFR-15 | Ketersediaan target layanan beta: 99,5% per bulan. |
| NFR-16 | Semua perubahan skema lewat migrasi yang bisa di-rollback. |

### 4.5 Keamanan dan privasi

| ID | Requirement |
|---|---|
| NFR-17 | Konten dari web dan supplier diperlakukan sebagai data tak tepercaya di seluruh pipeline. |
| NFR-18 | Data pribadi minimal; pengguna bisa mengekspor dan menghapus akunnya (UU PDP). Penghapusan mencakup KG user. |
| NFR-19 | Rahasia (API key) hanya di secret manager; tidak pernah di repo atau log. |
| NFR-20 | Batas pemakaian per pengguna dan per IP untuk mencegah penyalahgunaan biaya. |

### 4.6 Kemudahan perubahan

| ID | Requirement |
|---|---|
| NFR-21 | Mengganti model untuk satu peran cukup mengubah konfigurasi. |
| NFR-22 | Menambah supplier berarti menambah satu adapter yang memenuhi port; tidak ada perubahan di agen atau penyusun paket. |
| NFR-23 | Setiap perubahan prompt, model, atau logika paket dijalankan terhadap golden set sebelum rilis. |

## 5. Batasan sistem

| Batasan | Penjelasan |
|---|---|
| Backend Go | Ketetapan pemilik produk. |
| Persona pack berupa teks | Unit OpenPersona tidak membawa angka bertipe atau tanggal verifikasi. Angka acuan biaya disimpan terpisah di tabel `cost_norms`. |
| Memori user belum bisa dihapus | OpenPersona belum punya penghapusan scope. Memori tidak diaktifkan untuk pengguna sungguhan sebelum fitur itu ada (NFR-18). |
| Memori memakai SQLite satu berkas | Dijalankan sebagai satu layanan internal, bukan di dalam tiap instance `api`. |
| Guardrails menulis ulang payload | PII disamarkan sebelum model melihatnya dan dipulihkan di keluaran; narasi dikirim per paragraf, bukan per token. |
| OpenPersona dan guard-rails-engine pra-1.0 | Versi dipaku; kenaikan versi melewati golden set. |
| AI Lab belum dilihat | Kontrak event di `AI_SYSTEM_DESIGN.md` §11 masih usulan. |
| AI eksternal | JEV (TypeSafe AI) untuk keputusan; OpenAI untuk generasi. JEV: konteks 32 ribu token, hanya teks, hanya API ter-hosting, SDK resmi hanya Python/JS (dari Go lewat HTTP). |
| Supplier penerbangan | Duffel sebagai sumber utama. Biaya pencarian berlebih di atas rasio 1500:1 per order. Cakupan LCC dan dukungan IDR belum terverifikasi. |
| Jendela penjualan tiket | Harga live tidak tersedia untuk tanggal yang terlalu jauh; batas pastinya diambil dari perilaku supplier, bukan di-hardcode. |
| Harga non-API | Makan, transport lokal, tiket masuk, visa hanya tersedia sebagai estimasi dari KG. |
| Kurs | Harga asing dikonversi ke rupiah dengan kurs harian; paket memakai penyangga kurs. |

## 6. Ruang lingkup implementasi

### Termasuk (MVP, Fase 0–1)

- Web chat dengan streaming, akses tamu dan login
- Mode tujuan → budget untuk destinasi Jepang
- Integrasi penerbangan (Duffel) dan penginapan (LiteAPI)
- KG persona lewat port, dengan stub berbasis berkas untuk Osaka
- Penyusun paket deterministik, verifikasi, narasi
- Event log untuk AI Lab; guardrail stub
- Deep link keluar dan pencatatan klik

### Termasuk (Fase 2)

- Mode budget → tujuan dan Indeks Biaya Destinasi
- Tanggal di luar jendela penjualan + pengingat
- Aktivitas dari marketplace (Viator/Klook)
- KG user dan memori lintas sesi
- Guardrails asli terpasang; gerbang evaluasi di CI

### Tidak termasuk

- Pembayaran, penerbitan tiket, refund, dan layanan purna jual
- Aplikasi mobile native
- Kanal WhatsApp/Telegram
- Perjalanan multi-negara dalam satu paket
- Perjalanan rombongan di atas 9 orang
- Pembangunan library KG, guardrails, dan AI Lab (dikerjakan pemilik produk)

## 7. Definisi istilah

| Istilah | Arti |
|---|---|
| Run | Satu siklus pemrosesan dari satu pesan pengguna sampai jawaban selesai. |
| TripState | State terstruktur percakapan: slot, asumsi, paket terpilih. |
| ContextPack | Hasil retrieval KG untuk satu run. |
| Offer | Snapshot penawaran dari supplier pada satu waktu. |
| Paket | Susunan offer dan estimasi yang membentuk satu perjalanan lengkap. |
| KG persona | Knowledge graph berisi pengetahuan travel agent profesional. |
| KG user | Knowledge graph berisi fakta dan preferensi satu pengguna. |
| Cost norm | Angka biaya acuan di KG (misalnya biaya makan per kali per kota per tingkat). |
| Indeks Biaya Destinasi | Cache harga agregat untuk menjawab budget → tujuan tanpa panggilan live. |
