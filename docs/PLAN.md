# AI Travel Agent — Riset & Rencana Teknis (Prebuild)

Tanggal: 14 November 2026 · Status: draft v1 untuk review CEO

> Dokumen ini adalah ringkasan riset dan keputusan awal. Rencana teknis yang berlaku ada di sepuluh dokumen rekayasa: [TECHNICAL_SPEC](TECHNICAL_SPEC.md), [SYSTEM_ARCHITECTURE](SYSTEM_ARCHITECTURE.md), [TECH_STACK](TECH_STACK.md), [DATABASE_SCHEMA](DATABASE_SCHEMA.md), [API_SPEC](API_SPEC.md), [AI_SYSTEM_DESIGN](AI_SYSTEM_DESIGN.md), [MODULE_SPEC](MODULE_SPEC.md), [PROJECT_STRUCTURE](PROJECT_STRUCTURE.md), [IMPLEMENTATION_PLAN](IMPLEMENTATION_PLAN.md), [ENGINEERING_STANDARDS](ENGINEERING_STANDARDS.md). Bila ada perbedaan, sepuluh dokumen itu yang dipakai.

---

## 1. Ringkasan eksekutif

**Produk.** Agen AI yang menjawab dua pertanyaan yang hari ini hanya bisa dijawab travel agent manusia:

1. *"Saya mau ke X — berapa budget yang dibutuhkan?"* (tujuan → budget)
2. *"Saya punya Rp Y — bisa ke mana saja?"* (budget → tujuan)

Hasilnya berupa beberapa **paket perjalanan** (penerbangan, hotel, aktivitas, makan, transport lokal) dengan rincian biaya per item, gratis, instan, dan boleh dipakai sekadar penasaran.

**Tiga keputusan arsitektur terpenting:**

| # | Keputusan | Alasan |
|---|---|---|
| 1 | **LLM tidak pernah menghitung atau mengarang harga.** LLM memahami permintaan, memilih tool, dan menulis narasi. Penyusunan paket dan penjumlahan biaya dilakukan kode Go yang deterministik. | Kepercayaan pada angka adalah inti produk. Satu total yang salah hitung merusak semuanya. |
| 2 | **Setiap angka punya asal-usul (provenance):** `live` (dari API, ada waktu ambil dan masa berlaku) atau `estimasi` (dari knowledge graph / rata-rata, ada sumber dan tanggal). UI menampilkan bedanya. | Kita tidak akan pernah punya harga live untuk semua hal (makan, kereta, tiket di luar jendela booking). Jujur soal itu lebih baik daripada terlihat presisi palsu. |
| 3 | **KG, guardrails, dan AI Lab masuk lewat port (interface) dengan stub.** Format ketiganya masih Anda kembangkan, jadi sistem inti tidak boleh bergantung pada bentuk finalnya. | Kita bisa membangun web dan sistem sampai matang sekarang, lalu menancapkan komponen Anda tanpa refactor. |

**Temuan riset yang mengubah rencana:**

- **Amadeus Self-Service API sudah mati** (kunci dinonaktifkan 17 Juli 2026). Pilihan paling realistis untuk penerbangan adalah **Duffel**.
- **Model bisnis Duffel menghukum pencarian tanpa booking**: gratis sampai rasio 1500 pencarian per order, lalu $0,005 per pencarian. Produk kita justru untuk orang yang "penasaran saja", jadi cache harga wajib ada sejak hari pertama.
- **JEV** adalah model keputusan dari **TypeSafe AI** (bukan Typesense): mengembalikan pilihan/skor/probabilitas, tidak bisa menulis teks. Sangat murah ($0,042 per 1 juta token input) dan cocok untuk routing, gating, dan verifikasi — tetapi terbukti bisa dibelokkan oleh teks injeksi, jadi tidak boleh jadi satu-satunya lapis pengaman.
- **gpt-4o-mini bukan lagi pilihan termurah** untuk tugas sederhana. gpt-5-nano lebih murah di input, output, dan terutama cached input.
- **"Musim gugur nanti" berarti musim gugur 2027**, di luar jendela penjualan tiket maskapai (umumnya sekitar 11 bulan ke depan). Sistem harus bisa memberi estimasi ketika harga live belum ada. Ini bukan kasus pinggiran; ini contoh query utama Anda.

---

## 2. Masalah, pengguna, dan posisi produk

**Masalah.** Orang tidak jadi liburan karena tidak tahu budgetnya cukup atau tidak. Bertanya ke travel agent butuh biaya, waktu, dan niat serius. OTA (Traveloka, tiket.com) menjawab "berapa harga tiket ini", bukan "uang saya cukup untuk perjalanan seperti apa".

**Pengguna awal.** Wisatawan Indonesia, berangkat dari kota besar Indonesia, berpikir dalam rupiah, tujuan luar negeri populer (mulai dari Jepang).

**Lanskap kompetitor:**

| Pemain | Yang mereka lakukan | Celah untuk kita |
|---|---|---|
| Mindtrip | Chat + peta, 11 juta POI, booking penerbangan dalam chat (Sabre, sejak Mei 2026) | Pasar AS/global, bukan rupiah, bukan budget-first |
| Layla | Itinerary dengan harga live via Skyscanner/Booking/GetYourGuide | Sama: global, tidak berangkat dari budget |
| Wanderlog | Pengatur itinerary, AI sebagai pelengkap | Bukan agen |
| Trip.com Trip.Planner | Planner terintegrasi inventori sendiri | Terikat inventori Trip.com |
| tiket.com (CRATER) | Asisten agentic di atas Microsoft Foundry, diumumkan Juli 2026 | Ancaman lokal terbesar; fokus inventori sendiri |
| Traveloka | Rekomendasi berbasis AI, masih search-and-filter | Belum ada planner budget-first yang terlihat |

**Posisi kita:** budget-first, rupiah, berangkat dari Indonesia, pengetahuan travel agent profesional di knowledge graph, dan jujur soal mana harga pasti dan mana estimasi. Kita netral terhadap supplier.

**Risiko strategis:** tiket.com dan Traveloka punya inventori dan pengguna. Keunggulan kita yang sulit ditiru cepat adalah kualitas knowledge graph persona dan mesin paket budget-first. Itu yang harus diprioritaskan, bukan jumlah integrasi.

---

## 3. Riset: sumber data dan supplier

### 3.1 Penerbangan

| Opsi | Akses | Biaya | Catatan |
|---|---|---|---|
| **Duffel** (rekomendasi) | Self-serve, daftar langsung | $3/order + 1% nilai order (managed content); pencarian berlebih $0,005 di atas rasio 1500:1; FX 2% | 300+ maskapai. Garuda tersedia lewat Travelport, Scoot lewat direct connect. **AirAsia dan rute CGK–KIX belum terverifikasi. Dukungan IDR belum terverifikasi.** |
| Amadeus Self-Service | **Mati sejak 17 Juli 2026** | — | Hanya Enterprise, berbasis permintaan |
| Kiwi Tequila | Hanya undangan | — | Tidak tersedia untuk startup baru |
| Travelpayouts | Afiliasi; dilaporkan butuh 50 ribu MAU untuk akses API | Komisi | Opsi nanti setelah ada traksi |
| SerpApi (Google Flights) | Self-serve | Per pencarian | Scraping, hanya tampilan harga, tidak bisa booking. Risiko ToS; hanya sebagai cadangan |

Contoh biaya pencarian Duffel: 50.000 pencarian dan 10 order dalam sebulan → 15.000 gratis, 35.000 × $0,005 = **$175**. Terkendali, asal ada cache.

**Konsekuensi desain:** tabel `price_cache` per (rute, tanggal, kabin) dengan TTL; pencarian live hanya untuk kandidat yang benar-benar masuk paket; mode "budget → tujuan" tidak pernah memanggil API live per destinasi.

### 3.2 Hotel

| Opsi | Akses | Model | Catatan |
|---|---|---|---|
| **LiteAPI (Nuitée)** (rekomendasi) | Sandbox gratis, self-serve | Komisi lewat parameter `margin`, dibayar setelah tamu check-out | Paling cepat untuk mulai. Ada klaim pesaing soal latensi lambat; ukur sendiri |
| Duffel Stays | Sama dengan akun Duffel | Bagi hasil komisi | Satu vendor untuk dua kebutuhan; tarif perlu tanya sales |
| Booking.com Demand API | Harus jadi Managed Affiliate Partner, ada kontrak | Komisi | Tidak realistis di tahap prebuild |
| Hotelbeds / RateHawk | Butuh hubungan komersial | Net rate | Fase lanjut |

### 3.3 Aktivitas dan tiket wisata

| Opsi | Akses | Catatan |
|---|---|---|
| **Viator Partner API (Basic)** | Self-serve setelah akun afiliasi, gratis | Konten dan harga; booking terjadi di viator.com. Akses booking penuh perlu persetujuan |
| Klook | Afiliasi (komisi sekitar 2–5%) atau partner API dengan persetujuan | Paling kuat di Asia-Pasifik; kejar kemitraan lebih awal |
| GetYourGuide | Partner API, akses terbatas | Fase lanjut |
| **KG persona** | Milik kita | Atraksi gratis, festival, tiket masuk kuil/museum, yang tidak dijual marketplace |

### 3.4 Yang tidak punya API: makan, transport lokal, visa, musim

Semuanya masuk **knowledge graph persona** sebagai *cost norm* dan aturan, dengan sumber dan tanggal berlaku. Contoh isi (nilai harus diisi dan diverifikasi oleh travel agent, bukan oleh LLM):

- Biaya makan per kali per kota per tingkat (hemat / sedang / nyaman)
- Biaya dan durasi transport antar-titik (bandara → pusat kota, antar-area)
- Aturan visa untuk paspor Indonesia per negara, beserta biayanya
- Kalender musim dan festival (jadwal festival berubah tiap tahun, jadi perlu `valid_for_year`)
- Heuristik agen: "jangan pindah hotel untuk perjalanan di bawah 5 malam", "hari pertama dan terakhir bukan hari wisata penuh"

### 3.5 Kurs

Satu penyedia kurs, di-cache harian. Semua harga disimpan dalam mata uang asli **dan** rupiah, dengan kurs dan tanggalnya. Paket memakai penyangga kurs (usul: 3%) karena pengguna membayar beberapa minggu atau bulan kemudian.

---

## 4. Riset: model AI dan rekomendasi routing

### 4.1 Harga (halaman harga resmi OpenAI, diambil 14 Nov 2026; USD per 1 juta token)

| Model | Input | Cached input | Output |
|---|---|---|---|
| gpt-5-nano | 0,05 | 0,005 | 0,40 |
| gpt-6-luna | 0,10 | 0,01 | 0,50 |
| gpt-4o-mini | 0,15 | 0,075 | 0,60 |
| gpt-5.6-luna | 0,20 | 0,02 | 1,20 |
| **gpt-5-mini** | 0,25 | 0,025 | 2,00 |
| gpt-5.4-mini | 0,75 | 0,075 | 4,50 |
| gpt-6.1-terra | 1,00 | 0,05 | 5,00 |

Web search tool: $10 per 1.000 panggilan + token konten dengan tarif model. Embedding `text-embedding-3-small`: $0,02.

JEV (TypeSafe AI): $0,042 input, output gratis, konteks 32 ribu token, hanya teks, hanya API ter-hosting.

### 4.2 Rekomendasi routing

| Tugas | Model | Catatan |
|---|---|---|
| Klasifikasi intent, cek kelengkapan slot, in-scope atau tidak, bahasa | **JEV** | Beberapa pertanyaan dalam satu request, satu state. Hampir gratis |
| Gating tool call (boleh jalan atau tidak) | **JEV** + aturan kode | JEV sebagai sinyal, bukan hakim tunggal |
| Verifikasi keluaran terhadap rubrik ("syarat festival terpenuhi?", "klaim ini didukung data?") | **JEV** | Pakai ambang per pertanyaan dari data berlabel |
| Ekstraksi slot ke JSON terstruktur, ringkasan percakapan, judul sesi | **gpt-5-nano** | Menggantikan gpt-4o-mini: lebih murah, cached input 15× lebih murah |
| Loop perencana (memilih tool, menalar trade-off) | **gpt-5-mini** | Sesuai pilihan Anda |
| Riset web (festival tahun ini, penutupan, harga yang tidak ada di KG) | **gpt-5-mini + web search** | Sub-panggilan terisolasi, hasil di-cache dan diberi sumber |
| Narasi akhir paket | **gpt-5-mini** | Streaming |

**Catatan jujur:**

- **gpt-4o-mini sebaiknya diganti gpt-5-nano.** Satu hal yang perlu diuji: gpt-5-nano adalah model reasoning, jadi latensinya harus diukur dengan reasoning effort minimal.
- **Keluarga gpt-6 dan gpt-5.6 ada di halaman harga**, dan gpt-6-luna lebih murah dari gpt-4o-mini. Saya belum punya data kualitasnya untuk kasus kita. Jangan pilih dari tabel harga saja; biarkan AI Lab membandingkannya di golden set (bagian 10). Arsitektur membuat penggantian model cukup lewat konfigurasi.
- **Batasan JEV yang harus kita tangani:** (a) dalam pengujian pihak ketiga, teks injeksi membalik jawaban dengan confidence di atas 70%, jadi teks tak tepercaya (hasil web, deskripsi hotel) ditaruh di field terpisah dan tidak boleh menentukan keputusan keamanan sendirian; (b) pin versi (`jev-1.13`, bukan `jev-latest`); (c) konteks 32 ribu token, jadi kirim state ringkas, bukan seluruh percakapan; (d) SDK resminya Python dan JavaScript, dari Go kita panggil endpoint HTTP-nya.

### 4.3 Perkiraan biaya per rencana (perkiraan kasar, harus diukur)

Asumsi: loop perencana ±80 ribu token input (60% ter-cache) dan 8 ribu token output di gpt-5-mini, 2 pencarian web, JEV dan nano untuk sisanya.

| Komponen | USD |
|---|---|
| gpt-5-mini input + cached + output | ±0,025 |
| Web search (2 panggilan + konten) | ±0,024 |
| JEV + gpt-5-nano | < 0,002 |
| **Total** | **±0,05–0,10** (sekitar Rp 800–1.700 pada asumsi kurs Rp 16.500) |

Biaya LLM bukan masalah. Yang perlu dijaga adalah biaya pencarian supplier dan jumlah pencarian web per sesi (beri batas keras).

---

## 5. Arsitektur sistem

### 5.1 Gambaran

```
 Browser (Next.js)
    │  HTTPS + SSE (streaming)
    ▼
┌──────────────────────────── Go: api ────────────────────────────┐
│ Auth · Session · Rate limit · SSE                               │
│                                                                  │
│  ┌──────────── Orchestrator (pipeline) ──────────────┐          │
│  │ 1 Guardrail-in → 2 Route (JEV) → 3 Ekstraksi slot │          │
│  │ → 4 Retrieval KG (pipeline) → 5 Agent loop        │          │
│  │ → 6 Package Builder → 7 Verifier → 8 Narasi       │          │
│  └───────────────────────────────────────────────────┘          │
│        │            │             │            │                 │
│   port:Model   port:Retriever  port:Guardrail  port:Tracer       │
│   (OpenAI,JEV) (KG Anda)       (milik Anda)    (AI Lab Anda)     │
│        │                                                         │
│   port:Supplier → Duffel · LiteAPI · Viator · Kurs               │
└──────────────────────────────────────────────────────────────────┘
    │                         │
 Postgres                   Redis            Go: worker
 (data, event log,          (cache harga,    (refresh cache harga,
  job queue)                 sesi, limit)     ingest KG, ringkasan)
```

### 5.2 Pilihan teknologi

| Lapisan | Pilihan | Alasan |
|---|---|---|
| Backend | **Go, modular monolith** (dua binary: `api`, `worker`) | Tim kecil; batas modul jelas di `internal/`, bisa dipecah nanti. Microservices sekarang hanya menambah beban operasional |
| Agent runtime | **Loop tipis buatan sendiri** di atas SDK resmi `openai-go` | Anda punya guardrails dan AI Lab sendiri yang perlu hook di tiap langkah loop. Framework (Eino, Genkit, ADK) menyembunyikan loop itu; Genkit bahkan masih menandai API agennya eksperimental |
| Database | **Postgres** | Data relasional, event log, dan antrean job (River) dalam satu tempat |
| Cache | **Redis** | Cache harga, state sesi panas, rate limit |
| Frontend | **Next.js + TypeScript** | Chat streaming, kartu paket, timeline itinerary |
| Streaming | **SSE** | Satu arah, sederhana, cukup untuk chat dan progres tool |
| Observability | **OpenTelemetry** + tabel `run_events` | `run_events` adalah kontrak data ke AI Lab |
| Kontrak API | **OpenAPI**, tipe TS digenerate | Frontend dan backend tidak bisa melenceng |

### 5.3 Port (inti dari "sistem paling matang")

```go
type ModelProvider interface {            // OpenAI, JEV, atau lainnya
    Generate(ctx, GenerateRequest) (GenerateStream, error)
    Decide(ctx, DecideRequest) (DecideResult, error)   // gaya JEV
}

type Retriever interface {                // library KG Anda
    RetrievePipeline(ctx, scope Scope, req TripRequest) (ContextPack, error)
    Query(ctx, scope Scope, q KGQuery) (KGResult, error)   // dipakai tool di loop
}                                         // Scope = Persona | User(userID)

type Guardrail interface {                // guardrails Anda
    Check(ctx, point HookPoint, payload any) (Verdict, error)
}                                         // HookPoint: Input, PreTool, PostTool, Output

type Tracer interface {                   // AI Lab Anda
    Emit(ctx, RunEvent)
}

type FlightSupplier interface { Search(ctx, FlightQuery) ([]FlightOffer, error) }
type StaySupplier   interface { Search(ctx, StayQuery) ([]StayOffer, error) }
type ActivitySupplier interface { Search(ctx, ActivityQuery) ([]ActivityOffer, error) }
```

Setiap port punya implementasi stub sejak minggu pertama (KG dari file JSON, guardrail yang selalu lolos, tracer ke Postgres), sehingga seluruh alur bisa jalan sebelum komponen Anda siap.

### 5.4 Model data inti

| Tabel | Isi |
|---|---|
| `users`, `sessions`, `messages` | Identitas dan percakapan |
| `trip_requests` | Slot terstruktur: asal, tujuan, budget, tanggal/musim, jumlah orang, minat, asumsi |
| `offers` | Snapshot penawaran supplier: harga, mata uang, `fetched_at`, `expires_at`, payload mentah |
| `packages`, `package_items` | Paket dan itemnya; tiap item punya `price_kind` (`live`/`estimate`), sumber, kurs |
| `price_cache` | Harga termurah per rute/kota/bulan untuk mode budget → tujuan |
| `runs`, `run_events` | Satu run per giliran agen; event per langkah (model, token, biaya, argumen dan hasil tool, latensi, putusan guardrail) |

---

## 6. Rancangan agen

### 6.1 Pipeline per giliran

| Langkah | Pelaku | Keluaran |
|---|---|---|
| 1. Guardrail masuk | Guardrail Anda | Lolos / tolak |
| 2. Routing | JEV (satu request, banyak pertanyaan) | Intent, in-scope, slot mana yang sudah ada |
| 3. Ekstraksi slot | gpt-5-nano, keluaran terstruktur | `TripRequest` diperbarui |
| 4. Klarifikasi (bila perlu) | Aturan kode | Maksimal satu putaran pertanyaan; sisanya diasumsikan dan asumsinya ditampilkan |
| 5. Retrieval pipeline | Retriever (KG persona + KG user) | `ContextPack`: destinasi, musim, festival, cost norm, preferensi user |
| 6. Loop agen | gpt-5-mini + tools | Kandidat penawaran dan rencana harian |
| 7. Penyusun paket | **Kode Go deterministik** | 2–3 paket dengan rincian biaya |
| 8. Verifikasi | Kode (invarian) + JEV (rubrik) | Lolos / perbaiki / tandai |
| 9. Narasi | gpt-5-mini, streaming | Penjelasan untuk pengguna |
| 10. Guardrail keluar | Guardrail Anda | — |

Aturan klarifikasi penting untuk pengguna yang "penasaran saja": jangan menginterogasi. Yang wajib hanya **budget atau tujuan** (salah satu). Asal default dari profil, jumlah orang default 1, durasi default dari heuristik KG.

### 6.2 Tools di dalam loop

| Tool | Sumber | Catatan |
|---|---|---|
| `search_flights` | FlightSupplier + cache | Filter: asal, tujuan, tanggal fleksibel, kabin, maks transit, bagasi |
| `search_stays` | StaySupplier | Filter: area, tanggal, tamu, rentang harga, rating |
| `search_activities` | ActivitySupplier + KG | Gabungan marketplace dan KG |
| `kg_query` | Retriever (persona) | Retrieval "on loop" |
| `estimate_local_costs` | KG cost norm | Makan × jumlah kali makan, transport antar-titik |
| `web_research` | gpt-5-mini + web search | Terisolasi; mengembalikan fakta + URL; dibatasi per sesi |
| `convert_currency` | Penyedia kurs | — |
| `build_packages` | Penyusun paket | Menerima ID kandidat, bukan angka |
| `get_user_memory` | Retriever (user) | — |

Model **hanya mengoper ID penawaran** ke `build_packages`. Ia tidak pernah menulis harga sebagai argumen, jadi tidak bisa salah salin atau mengarang.

### 6.3 Penyusun paket (deterministik)

1. Ambil kandidat: beberapa penerbangan (termurah, tercepat, seimbang), beberapa hotel per tingkat, rencana harian dari KG.
2. Hitung biaya tetap: penerbangan + visa + asuransi (opsional) + transport bandara.
3. Hitung biaya harian: hotel + makan + transport lokal + aktivitas.
4. Bentuk tiga profil: **Hemat**, **Seimbang**, **Nyaman**, masing-masing ≤ budget setelah penyangga (usul: 10% tak terduga + 3% kurs).
5. Bila tidak ada yang muat, **katakan terus terang** dan beri jalan keluar terurut: geser tanggal, kurangi malam, ganti area menginap, ganti destinasi, atau "butuh tambahan sekitar Rp X".
6. Keluaran: paket + rincian per item + `price_kind` + asumsi yang dipakai.

### 6.4 Mode budget → tujuan

Tidak bisa memanggil API live untuk puluhan destinasi. Solusinya **Indeks Biaya Destinasi**: worker mengisi `price_cache` (harga penerbangan terendah per rute per bulan, median hotel per kota per tingkat) dan KG menyediakan biaya harian. Query dijawab dari indeks, lalu harga live diambil hanya untuk 2–3 destinasi yang dipilih pengguna.

### 6.5 Tanggal di luar jendela booking

Bila tanggal belum dijual (kasus "musim gugur nanti"): pakai harga periode setara yang sudah tersedia atau data cache, tandai `estimate`, jelaskan kapan harga live mulai tersedia, dan tawarkan pengingat. Ini juga fitur retensi yang bagus.

### 6.6 Verifikasi

**Invarian kode (keras):** total = jumlah item; total ≤ budget; tanggal konsisten (check-in = tanggal tiba, bukan tanggal berangkat untuk penerbangan malam); tidak ada item `live` yang sudah kedaluwarsa; setiap harga punya sumber.

**Rubrik JEV (lunak):** apakah permintaan khusus terpenuhi (festival musim gugur di Osaka), apakah setiap klaim faktual di narasi didukung `ContextPack` atau hasil tool. Di bawah ambang → perbaiki sekali, lalu tandai untuk AI Lab.

---

## 7. Manajemen konteks sesi (ringan tapi bagus)

Prinsip: **state terstruktur adalah sumber kebenaran, bukan riwayat chat.**

| Lapis | Isi | Ukuran | Diperbarui |
|---|---|---|---|
| 1. Prefix stabil | System prompt, persona, definisi tool | Tetap | Jarang (agar prompt cache kena) |
| 2. `TripState` | Slot, batasan, asumsi, paket terpilih | Ratusan token | Tiap giliran, oleh kode |
| 3. `ContextPack` | Hasil retrieval KG untuk giliran ini | Dibatasi anggaran token | Tiap giliran |
| 4. Jendela percakapan | N giliran terakhir, verbatim | Dibatasi | Bergulir |
| 5. Ringkasan | Giliran yang lebih lama | Pendek | Asinkron oleh gpt-5-nano |
| 6. Artefak di luar konteks | Hasil tool lengkap disimpan di DB; konteks hanya memuat ringkasan + ID | — | Tiap panggilan tool |

Hasil pencarian penerbangan mentah bisa puluhan ribu token. Model hanya melihat tabel ringkas (ID, maskapai, jam, transit, harga); detailnya diambil kode lewat ID. Inilah yang membuat konteks tetap ringan.

**Memori lintas sesi** masuk KG user: di akhir sesi, ekstraksi fakta yang tahan lama (kota asal, gaya perjalanan, pantangan makan, rentang budget) dengan persetujuan pengguna, lalu diambil di langkah retrieval pipeline. Pengguna bisa melihat dan menghapusnya.

---

## 8. Knowledge graph

Format mengikuti library Anda. Yang saya usulkan hanya **isi dan kontrak**.

**KG persona (pengetahuan travel agent):**

| Jenis node | Contoh atribut |
|---|---|
| Destinasi, Area | Karakter, cocok untuk siapa, area menginap yang disarankan |
| Musim, Event/Festival | Rentang tanggal, `valid_for_year`, keramaian, dampak harga |
| Atraksi | Biaya masuk, durasi kunjungan, jam buka, area |
| TransportLink | Dari–ke, moda, biaya, durasi |
| CostNorm | Makan/transport per kota per tingkat, mata uang, tanggal berlaku |
| AturanVisa | Per paspor per negara, biaya, waktu proses |
| Heuristik | Aturan praktis agen, dengan kondisi berlakunya |

Setiap fakta wajib punya **sumber, tanggal verifikasi, dan tingkat keyakinan**. Fakta harga yang lebih tua dari ambang tertentu otomatis ditandai untuk diverifikasi ulang.

**Proses pengisian:** wawancara terstruktur dengan travel agent profesional → draf terstruktur (boleh dibantu LLM) → **review manusia** → masuk KG. LLM tidak boleh menulis ke KG persona tanpa review.

**Mulai sempit:** Jepang (Osaka, Kyoto, Tokyo) sampai kualitasnya benar-benar bagus, baru melebar. Kedalaman satu destinasi lebih meyakinkan daripada 50 destinasi dangkal.

---

## 9. Guardrails dan keamanan

Guardrails Anda masuk di empat titik hook (masuk, sebelum tool, sesudah tool, keluar). Di luar itu, yang dijamin sistem inti:

- **Konten tak tepercaya** (hasil web, deskripsi dari supplier) diperlakukan sebagai data, bukan instruksi; ditandai dan dipisahkan di prompt maupun di state JEV.
- **Batas keras per sesi:** jumlah langkah loop, pencarian web, pencarian supplier, dan biaya.
- **Tidak ada pembayaran dan data kartu di MVP** (lihat bagian 12), sehingga tidak ada beban PCI.
- **Data pribadi:** minimal, terenkripsi, dengan hak hapus (UU PDP).
- **Disclaimer produk:** harga estimasi bukan penawaran; aturan visa harus dicek ke sumber resmi.

---

## 10. Evaluasi dan AI Lab

`run_events` adalah kontraknya: setiap langkah loop memancarkan event bertipe dengan `run_id`, langkah, model, token, biaya, latensi, argumen dan hasil tool, serta putusan guardrail. AI Lab Anda membaca dari sana.

**Golden set** (mulai ±50 query, dalam bahasa Indonesia sehari-hari): budget cukup, budget kurang, tanggal samar, permintaan khusus (festival), budget → tujuan, di luar cakupan, percobaan injeksi.

| Metrik | Target awal |
|---|---|
| Akurasi budget (total paket vs harga saat dicek ulang) | Dalam ±10% untuk item `live` |
| Kepatuhan batasan (budget, tanggal, permintaan khusus) | ≥ 95% |
| Klaim faktual yang punya sumber | ≥ 95% |
| Waktu ke token pertama | < 2 detik |
| Waktu sampai paket lengkap | < 45 detik (p95) |
| Biaya per rencana | < $0,15 |

Setiap perubahan prompt atau model dijalankan ke golden set sebelum rilis.

---

## 11. Roadmap

**Fase 0 — Keputusan dan spike (2 minggu)**

- Spike Duffel: cari CGK–KIX, cek maskapai yang muncul (terutama LCC), mata uang, latensi
- Spike LiteAPI: hotel Osaka, latensi, kelengkapan data
- Spike JEV: 100 query berlabel untuk routing; ukur akurasi, latensi, dan ambang
- Adapter percobaan untuk library KG Anda di balik port `Retriever`
- Keputusan CEO di bagian 13

**Fase 1 — Irisan vertikal Osaka (6 minggu)**

- Kerangka repo, CI, skema DB, auth, sesi, SSE
- Semua port + stub; loop agen; tools penerbangan/hotel/KG
- Penyusun paket + invarian; UI chat + kartu paket + rincian biaya
- `run_events` mengalir ke AI Lab
- **Kriteria selesai:** query Osaka dari brief Anda menghasilkan tiga paket yang lolos semua invarian

**Fase 2 — Pematangan (6 minggu)**

- Mode budget → tujuan (Indeks Biaya Destinasi)
- Penanganan tanggal di luar jendela booking + pengingat
- KG user dan memori lintas sesi; aktivitas (Viator/Klook)
- Golden set + gerbang evaluasi di CI; guardrails Anda terpasang
- Beta tertutup

**Fase 3 — Monetisasi dan perluasan**

- Deep link afiliasi → kemudian booking dalam aplikasi
- Destinasi tambahan; kemitraan Klook dan OTA lokal

**Tim minimum:** 2 backend Go (satu fokus agen), 1 frontend, 1 kurator KG (latar travel agent, paruh waktu boleh), CTO merangkap arsitek.

### Struktur repo yang diusulkan

```
/cmd/api, /cmd/worker
/internal/agent        loop, tools, prompt
/internal/pipeline     orkestrasi langkah 1–10
/internal/packagebuilder
/internal/ports        interface (model, retriever, guardrail, tracer, supplier)
/internal/adapters     openai, jev, duffel, liteapi, viator, kgstub, ...
/internal/session      TripState, konteks, ringkasan
/internal/store        Postgres, Redis
/web                   Next.js
/eval                  golden set + runner
/docs
```

---

## 12. Risiko utama

| Risiko | Dampak | Mitigasi |
|---|---|---|
| Duffel tidak memuat LCC kunci (AirAsia) untuk rute Indonesia | Harga paket lebih mahal dari kenyataan | Spike di Fase 0; cadangan sumber harga kedua; label "harga bisa lebih murah di LCC" dari KG |
| Biaya pencarian membengkak karena rasio look-to-book | Tagihan supplier | Cache, batas per sesi, live hanya untuk kandidat akhir |
| Harga berubah antara rencana dan booking | Kepercayaan turun | `expires_at`, tombol "cek ulang harga", penyangga |
| KG persona dangkal atau usang | Produk tidak beda dari ChatGPT | Kurator khusus, tanggal verifikasi, mulai sempit |
| OTA lokal merilis fitur serupa | Tekanan pasar | Netral supplier, budget-first, kedalaman pengetahuan |
| Model berubah atau dihentikan | Regresi | Port `ModelProvider`, pin versi, golden set |
| Injeksi lewat konten web/supplier | Keluaran menyesatkan | Pemisahan data tak tepercaya, guardrails, invarian kode |

---

## 13. Keputusan yang saya butuhkan dari Anda

1. **Model bisnis MVP.** Saran saya: afiliasi/deep link dulu, tanpa pembayaran dalam aplikasi. Booking dalam aplikasi menuntut izin usaha perjalanan, penanganan refund, dan layanan pelanggan.
2. **Cakupan awal.** Saran saya: hanya Jepang, berangkat dari Jakarta/Surabaya/Denpasar.
3. **Web saja atau juga WhatsApp?** Saran saya: web dulu; WhatsApp sebagai kanal kedua karena arsitekturnya sudah memisahkan kanal dari agen.
4. **Siapa sumber pengetahuan travel agent** dan berapa waktu mereka per minggu? Ini jalur kritis, bukan kodenya.
5. **Kontrak tiga komponen Anda** (library KG, guardrails, AI Lab): cukup bentuk kasar input/output-nya agar port di bagian 5.3 bisa disesuaikan sebelum Fase 1.

---

## Sumber

- Amadeus: [PhocusWire — shutdown portal self-service](https://www.phocuswire.com/amadeus-shut-down-self-service-apis-portal-developers) · [Skift, 3 Sep 2026](https://skift.com/2026/09/03/amadeus-brings-travel-tech-into-claude/)
- Duffel: [Pricing](https://duffel.com/pricing) · [Garuda Indonesia](https://duffel.com/flights/airlines/garuda-indonesia) · [Scoot](https://duffel.com/blog/scoot-is-available-with-duffel)
- Perbandingan API penerbangan: [Geekflare](https://geekflare.com/guides/flight-booking-api/) · [ScrapingBee](https://www.scrapingbee.com/blog/top-flights-apis-for-travel-apps/)
- Hotel: [LiteAPI — komisi](https://docs.liteapi.travel/docs/revenue-management-and-commission) · [LiteAPI dev](https://dev.liteapi.travel/) · [Booking.com Demand API — prasyarat](https://developers.booking.com/demand/docs/getting-started/prerequisites)
- Aktivitas: [Viator Partner API](https://docs.viator.com/partner-api/) · [Viator affiliate](https://partnerresources.viator.com/travel-commerce/affiliate/) · [Klook affiliate](https://affilimate.com/programs/klook-affiliate-program/)
- Model: [OpenAI pricing](https://developers.openai.com/api/docs/pricing) · [JEV di Vercel AI Gateway](https://vercel.com/ai-gateway/models/jev.md) · [Respan — ulasan JEV](https://www.respan.ai/articles/what-is-the-jev-ai-model) (ditulis vendor pesaing)
- Framework Go: [Encore — perbandingan](https://encore.dev/articles/best-go-framework-for-building-ai-agents) · [DZone — Genkit vs ADK](https://dzone.com/articles/go-agents-genkit-vs-adk)
- Kompetitor: [tiket.com × Microsoft](https://news.microsoft.com/source/asia/2026/07/14/kolaborasi-tiket-com-microsoft-dorong-layanan-travel-yang-lebih-seamless-dengan-ai/?lang=id) · [Traveloka — SWA](https://swa.co.id/read/474460/traveloka-memanfaatkan-akal-imitasi-untuk-menangkap-perilaku-wisatawan-ke-destinasi-wisata-asia) · [Trip.com Trip.Planner](https://www.altexsoft.com/travel-industry-news/trip-com-launches-ai-trip-planner-for-custom-itineraries/)

Banyak perbandingan supplier berasal dari blog vendor. Angka harga Duffel dan OpenAI diambil dari halaman resmi; sisanya perlu dikonfirmasi di Fase 0.
