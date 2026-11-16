# ENGINEERING_STANDARDS.md — Standar Rekayasa

Dokumen 10 dari 10 · Draft v1 · 14 November 2026

---

## 1. Konvensi kode

### 1.1 Go

| Hal | Aturan |
|---|---|
| Format | `gofumpt` + `goimports`; tidak diperdebatkan |
| Lint | `golangci-lint` dengan konfigurasi repo; nol peringatan di `main` |
| Gaya | Effective Go dan Go Code Review Comments |
| Konteks | `ctx` parameter pertama; tidak disimpan di struct; setiap panggilan I/O menerima `ctx` |
| Waktu | Tidak ada `time.Now()` di logika; pakai `platform/clock` yang disuntikkan |
| ID | Dari `platform/id`; tidak dibuat di SQL |
| Global | Tidak ada state global yang bisa berubah; ketergantungan lewat konstruktor |
| Goroutine | Setiap goroutine punya pemilik, jalan keluar, dan pemulihan panic di batasnya |
| Uang | Hanya tipe `domain.Money`; `float` dilarang untuk uang |
| Interface | Didefinisikan di sisi pemakai (`ports`); kecil; tidak dibuat sebelum ada dua implementasi atau kebutuhan uji |
| Komentar | Menjelaskan *mengapa*; setiap tipe dan fungsi yang diekspor punya komentar dok |
| Ukuran | Fungsi yang sulit dibaca dalam satu layar dipecah; berkas di atas ±500 baris ditinjau |

### 1.2 TypeScript / web

| Hal | Aturan |
|---|---|
| Kompiler | `strict: true`; tanpa `any` kecuali di batas dengan alasan tertulis |
| Tipe API | Hanya dari hasil generate |
| Komponen | Fungsi; state server lewat TanStack Query; state lokal sekecil mungkin |
| Uang dan tanggal | Diformat lewat `lib/format`; tidak ada aritmetika uang di klien |
| Teks | Lewat kamus i18n; tidak ada string pengguna yang ditulis langsung di komponen |
| Aksesibilitas | Elemen semantik, label, navigasi keyboard, kontras memadai |

### 1.3 SQL

- Query ditulis tangan di `store/postgres/queries/`, digenerate `sqlc`.
- Tidak ada `select *`.
- Setiap query baru pada tabel besar ditinjau rencana eksekusinya.
- Transaksi dibuka di lapisan aplikasi lewat helper `store.WithTx`; sesingkat mungkin; tanpa panggilan jaringan di dalamnya.

### 1.4 Git

| Hal | Aturan |
|---|---|
| Cabang | Trunk-based; cabang pendek dari `main` |
| Commit | Conventional Commits (`feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`) |
| PR | Kecil dan satu tujuan; deskripsi menyebut apa, mengapa, cara uji |
| Tinjauan | Minimal satu persetujuan; perubahan `packagebuilder`, `verifier`, prompt, dan migrasi butuh persetujuan pemilik modul |
| `main` | Selalu bisa dirilis |

## 2. Pola desain yang dipakai

| Pola | Di mana | Catatan |
|---|---|---|
| Ports and adapters | Seluruh I/O | Lihat `PROJECT_STRUCTURE.md` §2 |
| Dependency injection lewat konstruktor | `app/` | Tanpa kerangka DI |
| Fungsi murni untuk logika inti | `packagebuilder`, invarian | Masukan → keluaran, tanpa efek samping |
| Pipeline langkah | `pipeline` | `func(ctx, *RunState) error` |
| Registry | Tools agen | Tool mendaftar; loop tidak tahu implementasinya |
| Dekorator | Adapter supplier | Cache, retry, circuit breaker, tracing membungkus adapter dasar |
| Optimistic locking | TripState | Kolom versi |
| Outbox ringan | Event → AI Lab | Event tersimpan dulu, dikirim kemudian |
| Idempotency key | Pembuatan run | — |

Pola yang dihindari: warisan lewat embedding untuk berbagi perilaku, "manager"/"helper" serba bisa, abstraksi sebelum ada kebutuhan kedua, logika bisnis di handler HTTP.

## 3. Penanganan error

### 3.1 Aturan

1. Error dibungkus dengan konteks: `fmt.Errorf("search flights: %w", err)`.
2. Pemeriksaan dengan `errors.Is` / `errors.As`; tidak pernah membandingkan teks.
3. Error bertipe didefinisikan di `ports` (I/O) dan `domain` (aturan); adapter menerjemahkan error pihak ketiga ke tipe ini.
4. Error ditangani **sekali**: dicatat di tempat ia diputuskan, tidak di setiap lapisan.
5. `panic` hanya untuk kesalahan pemrogram; dipulihkan di batas request dan batas run.
6. Tidak ada error yang diabaikan diam-diam; pengabaian yang disengaja diberi komentar.

### 3.2 Klasifikasi

| Kelas | Contoh | Perilaku |
|---|---|---|
| Validasi | Input pengguna salah | `422`; tidak dicatat sebagai error |
| Tidak ditemukan / bukan pemilik | — | `404` |
| Konflik | Run aktif, versi usang | `409` |
| Sementara (hulu) | Timeout supplier, `429` model | Retry sekali dengan jitter → degradasi |
| Tetap (hulu) | Kunci API salah, query ditolak | Tanpa retry; error + peringatan |
| Batas | Kuota, batas run | `429` atau penghentian run yang rapi |
| Invarian dilanggar | Total paket tidak cocok | Error tingkat tinggi; paket tidak ditampilkan; peringatan |
| Internal | Bug | `500`; pelacakan error |

### 3.3 Degradasi

Urutan preferensi saat ketergantungan gagal: cache segar → cache usang berlabel → estimasi dari indeks/KG berlabel → menyatakan tidak tersedia. Sistem tidak pernah mengganti data yang hilang dengan tebakan model.

### 3.4 Pesan ke pengguna

Pesan error untuk pengguna berasal dari kamus berdasarkan `code`, dalam bahasa pengguna, tanpa detail internal. Detail teknis hanya di log dengan `request_id`.

## 4. Logging

| Hal | Aturan |
|---|---|
| Pustaka | `log/slog`, keluaran JSON |
| Field wajib | `time`, `level`, `msg`, `request_id`; bila ada: `run_id`, `conversation_id`, `user_hash` |
| Level | `debug` (lokal), `info` (kejadian bisnis), `warn` (degradasi, retry), `error` (butuh perhatian) |
| Pesan | Kalimat tetap dalam bahasa Inggris; data di field, bukan di string |
| Data pribadi | Tidak ada email, isi pesan, atau token di log. `user_id` di-hash. |
| Rahasia | Tidak pernah dicatat; header `Authorization` dan kunci API disaring di klien HTTP |
| Payload model/supplier | Tidak di log; tempatnya `run_events` dan `tool_artifacts` dengan retensi sendiri |
| Korelasi | `request_id` dan `trace_id` ada di setiap baris |

Aturan praktis: `error` berarti seseorang perlu melihatnya. Bila tidak, itu `warn` atau `info`.

### Metrik wajib

| Metrik | Label |
|---|---|
| `run_duration_seconds` | `mode`, `status` |
| `run_step_duration_seconds` | `step` |
| `llm_tokens_total`, `llm_cost_usd_total` | `role`, `model`, `kind` |
| `supplier_requests_total`, `supplier_latency_seconds` | `supplier`, `outcome` |
| `cache_requests_total` | `cache`, `outcome` |
| `runs_active` | — |
| `invariant_violations_total` | `invariant` |
| `guardrail_verdicts_total` | `point`, `verdict` |
| `outbound_clicks_total` | `supplier` |

Peringatan awal: `invariant_violations_total > 0`, tingkat run gagal di atas ambang, p95 latensi di atas target, biaya harian di atas anggaran, circuit breaker terbuka.

## 5. Strategi pengujian

| Lapis | Sasaran | Alat | Aturan |
|---|---|---|---|
| Unit | `domain`, `packagebuilder`, `verifier`, aturan klarifikasi, digest tool | `testing`, `testify` | Uji tabel; cepat; tanpa I/O |
| Properti | Invarian paket, aritmetika uang | `testing/quick` atau `rapid` | Masukan acak tidak boleh melanggar invarian |
| Golden | Keluaran `packagebuilder` untuk kasus tetap | Berkas golden | Perubahan keluaran harus ditinjau manusia |
| Kontrak adapter | Pemetaan respons supplier/model | Rekaman respons + server tiruan | Rekaman diperbarui terjadwal dari sandbox |
| Integrasi | `store`, `conversation`, SSE, job | `testcontainers-go` | Postgres dan Redis sungguhan; tanpa mock database |
| Pipeline | Run ujung ke ujung dengan model tiruan dan supplier terekam | Uji Go | Mencakup jalur gagal |
| Evaluasi AI | Kualitas dengan model sungguhan | `/eval` | Dijalankan pada perubahan prompt/model/logika paket dan terjadwal |
| Frontend unit | Komponen, pemformatan | Vitest | — |
| End-to-end | Alur utama | Playwright | Terhadap staging atau Compose dengan stub |
| Uji asap live | Supplier dan model sungguhan | Job terjadwal | Mendeteksi perubahan API hulu |

Target cakupan: `domain`, `packagebuilder`, `verifier` ≥ 90% baris; modul aplikasi lain ≥ 70%. Cakupan adalah lantai, bukan tujuan; setiap edge case di `MODULE_SPEC.md` punya uji bernama.

Aturan uji:

- Uji tidak memanggil jaringan luar di CI.
- Uji tidak bergantung pada jam atau urutan; jam disuntikkan.
- Bug yang ditemukan di produksi mendapat uji regresi sebelum diperbaiki.
- Model tiruan mengembalikan skrip tool call yang ditentukan; uji pipeline tidak boleh bergantung pada keluaran model sungguhan.

## 6. Standar khusus AI

| Hal | Aturan |
|---|---|
| Prompt | Berkas ber-versi di repo; perubahan lewat PR dengan hasil golden set terlampir |
| Model | Nama di konfigurasi; versi JEV dipaku; perubahan melewati golden set |
| Angka | Tidak ada jalur dari teks model ke harga; ditegakkan di tool, penyusun paket, dan `check` database |
| Konten tak tepercaya | Ditandai sejak adapter; dibungkus tag berlabel di prompt; tidak pernah digabung dengan instruksi |
| Batas | Setiap panggilan model punya timeout dan batas token keluaran; setiap run punya batas biaya |
| Rekaman | Setiap panggilan model dan tool menghasilkan event; tanpa event berarti belum selesai |
| Keluaran terstruktur | Selalu divalidasi terhadap skema; gagal validasi = ulang sekali lalu jalur cadangan |
| Eksperimen | Perbandingan model dan prompt dilakukan lewat `/eval` dan AI Lab, bukan di produksi tanpa pembanding |

## 7. Praktik keamanan

### 7.1 Aplikasi

| Area | Aturan |
|---|---|
| Autentikasi | Cookie `HttpOnly`, `Secure`, `SameSite=Lax`; token disimpan sebagai hash; rotasi saat login |
| CSRF | Token wajib untuk request yang mengubah data |
| Otorisasi | Setiap akses sumber daya memeriksa kepemilikan di lapisan aplikasi; bukan pemilik → `404` |
| Validasi | Semua input divalidasi di batas; batas ukuran body; batas panjang pesan |
| SQL | Hanya query berparameter (sqlc) |
| Keluaran web | Narasi dirender sebagai Markdown tersanitasi; tanpa HTML mentah; tautan keluar hanya dari URL offer |
| Header | CSP, HSTS, `X-Content-Type-Options`, `Referrer-Policy` |
| CORS | Hanya origin web kita |
| Rate limit | Per sesi, per IP, per endpoint mahal |
| Penyalahgunaan biaya | Kuota harian, batas run, pembatasan pembuatan sesi tamu |

### 7.2 Rahasia dan data

| Area | Aturan |
|---|---|
| Rahasia | Hanya di secret manager; tidak di repo, image, atau log; pemindai rahasia di CI |
| Enkripsi | TLS di semua koneksi; enkripsi saat diam untuk database dan cadangan |
| Data pribadi | Minimal; tujuan jelas; retensi sesuai `DATABASE_SCHEMA.md` §7 |
| Hak pengguna | Ekspor dan hapus akun berfungsi dan teruji, termasuk KG user |
| Pihak ketiga | Hanya data yang perlu dikirim ke model dan supplier; tanpa email atau identitas dalam prompt |
| Akses produksi | Berdasarkan peran; tercatat; tidak ada akses database langsung untuk pekerjaan rutin |

### 7.3 Rantai pasok

- Dependensi dikunci; pembaruan terjadwal; pemindaian kerentanan di CI (`govulncheck`, audit npm).
- Image dasar minimal; dipindai sebelum rilis.
- Dependensi baru butuh alasan di PR.

### 7.4 Sebelum beta

Tinjauan keamanan mencakup: model ancaman untuk injeksi prompt, uji otorisasi lintas pengguna, uji rate limit, pemeriksaan header, dan uji pemulihan dari cadangan.

## 8. Rilis dan operasi

| Hal | Aturan |
|---|---|
| Lingkungan | `local` → `staging` → `production` |
| Deploy | Otomatis dari `main` ke staging; ke produksi dengan persetujuan |
| Migrasi | Langkah terpisah sebelum binary baru; kompatibel mundur (expand → contract) |
| Rollback | Binary bisa dikembalikan satu versi tanpa rollback skema |
| Feature flag | Untuk fitur berisiko dan pergantian model; lewat konfigurasi |
| Cadangan | Database harian + point-in-time; pemulihan diuji berkala |
| Insiden | Dicatat; tinjauan pascainsiden tanpa menyalahkan; tindak lanjut jadi tugas |
| Runbook | Untuk: supplier mati, model mati, biaya melonjak, pelanggaran invarian |

## 9. Definition of done

Sebuah tugas selesai bila **semua** berikut terpenuhi:

**Kode**

- [ ] Memenuhi kriteria terima tugasnya
- [ ] Lint dan format bersih; aturan ketergantungan tidak dilanggar
- [ ] Kode hasil generate sinkron
- [ ] Tidak ada rahasia, data pribadi di log, atau `TODO` tanpa tiket

**Uji**

- [ ] Uji unit untuk logika baru, termasuk edge case di `MODULE_SPEC.md`
- [ ] Uji integrasi/kontrak bila menyentuh I/O
- [ ] Jalur gagal teruji, bukan hanya jalur sukses
- [ ] CI hijau

**Khusus perubahan AI** (prompt, model, tool, pipeline, penyusun paket)

- [ ] Event `run_events` ada untuk langkah baru
- [ ] Golden set dijalankan; hasil terlampir di PR; tidak ada metrik turun di bawah target
- [ ] Kasus golden baru ditambahkan untuk perilaku baru

**Operasi**

- [ ] Metrik dan log memadai untuk mendiagnosis kegagalan fitur ini
- [ ] Migrasi punya `Down` dan kompatibel mundur
- [ ] Konfigurasi baru ada di `.env.example` dengan nilai default yang aman

**Dokumentasi**

- [ ] Dokumen terkait di `/docs` diperbarui bila kontrak berubah
- [ ] `openapi.yaml` diperbarui bila API berubah

**Tinjauan**

- [ ] Disetujui peninjau; pemilik modul untuk area sensitif
- [ ] Sudah dicoba di staging untuk perubahan yang terlihat pengguna
