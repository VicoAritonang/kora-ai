# PROJECT_STRUCTURE.md — Struktur Proyek

Dokumen 8 dari 10 · Draft v1 · 14 November 2026

Satu repositori (monorepo) berisi backend Go, frontend web, kontrak API, migrasi, dan evaluasi.

---

## 1. Hierarki direktori

```
/
├── api/
│   └── openapi.yaml                 # kontrak API (sumber kebenaran)
│
├── cmd/
│   ├── api/main.go                  # binary HTTP: wiring + start
│   ├── worker/main.go               # binary job
│   └── eval/main.go                 # runner golden set
│
├── internal/
│   ├── domain/                      # tipe inti, Money, TripState, Package, aturan murni
│   ├── ports/                       # semua interface ke luar + error bertipe
│   │
│   ├── packagebuilder/              # penyusun paket (fungsi murni)
│   ├── verifier/                    # invarian + rubrik
│   ├── session/                     # TripState store, ContextBuilder
│   ├── agent/
│   │   ├── loop.go
│   │   ├── limits.go
│   │   └── prompts/                 # satu berkas per peran, ber-versi
│   ├── tools/                       # implementasi Tool (satu berkas per tool)
│   ├── pipeline/                    # langkah-langkah run + router + extractor
│   │
│   ├── conversation/                # percakapan, pesan, pembuatan run
│   ├── packages/                    # baca paket, pilih, cek ulang, tautan keluar
│   ├── priceindex/                  # Indeks Biaya Destinasi
│   ├── costnorm/                    # norma biaya terstruktur (tabel cost_norms)
│   ├── reminder/
│   ├── auth/
│   ├── usage/                       # kuota, rate limit, semaphore
│   │
│   ├── adapters/
│   │   ├── openai/
│   │   ├── jev/
│   │   ├── duffel/
│   │   ├── liteapi/
│   │   ├── viator/
│   │   ├── fx/
│   │   ├── openpersona/             # persona pack (in-process) + klien memori (HTTP)
│   │   ├── guardrails/              # pembungkus guard-rails-engine
│   │   ├── fakes/                   # implementasi palsu port, hanya untuk uji
│   │   ├── tracerpg/
│   │   └── tracerlab/               # penerus ke AI Lab
│   │
│   ├── store/
│   │   ├── postgres/
│   │   │   ├── queries/             # berkas .sql untuk sqlc
│   │   │   └── gen/                 # hasil sqlc (jangan diedit)
│   │   └── redis/
│   │
│   ├── transport/
│   │   └── http/
│   │       ├── gen/                 # hasil oapi-codegen (jangan diedit)
│   │       ├── handlers/            # satu berkas per kelompok endpoint
│   │       ├── middleware/
│   │       └── sse/
│   │
│   ├── jobs/                        # definisi job River
│   ├── config/                      # struct konfigurasi + pemuatan env
│   ├── app/                         # komposisi: merakit modul dari config
│   └── platform/                    # logging, tracing, clock, id, httpclient
│
├── migrations/                      # berkas SQL goose
│
├── persona/
│   ├── spec.json                    # brief persona (nama, peran, nada, batasan)
│   ├── sources/                     # dokumen markdown hasil wawancara travel agent
│   ├── questions/                   # pertanyaan uji untuk meninjau pack
│   └── packs/                       # hasil `openpersona train` (artefak ber-versi)
│
├── guardrails/
│   ├── policy.yaml                  # kebijakan, berlapis di atas profil balanced
│   └── tools.yaml                   # katalog tool agen (wajib untuk tahap Runtime)
│
├── data/
│   ├── norms/                       # berkas impor norma biaya per destinasi
│   └── reference/                   # kode bandara/kota, mata uang
│
├── eval/
│   ├── golden/                      # kasus uji (YAML)
│   ├── fixtures/                    # rekaman respons supplier/web
│   └── reports/                     # keluaran (diabaikan git)
│
├── web/                             # frontend Next.js
│   ├── src/
│   │   ├── app/                     # rute
│   │   ├── features/
│   │   │   ├── chat/
│   │   │   ├── packages/
│   │   │   ├── itinerary/
│   │   │   └── account/
│   │   ├── components/ui/           # komponen dasar
│   │   ├── lib/
│   │   │   ├── api/                 # klien + tipe hasil generate
│   │   │   ├── sse/
│   │   │   └── format/              # rupiah, tanggal
│   │   └── i18n/
│   └── tests/
│
├── deploy/
│   ├── docker/                      # Dockerfile api, worker, web
│   ├── compose/                     # docker-compose untuk lokal
│   └── infra/                       # IaC
│
├── docs/                            # sepuluh dokumen ini + PLAN.md
├── scripts/
├── .github/workflows/
├── Makefile
├── go.mod
├── sqlc.yaml
└── .golangci.yml
```

## 2. Batas modul dan aturan ketergantungan

```
transport/http ──► modul aplikasi ──► ports ◄── adapters
                        │               ▲
                        ▼               │
                     domain ◄───────────┘
                        ▲
                      store
```

| # | Aturan |
|---|---|
| D1 | `domain` tidak mengimpor paket internal mana pun dan tidak mengimpor pustaka I/O. |
| D2 | `ports` hanya mengimpor `domain`. |
| D3 | `packagebuilder` hanya mengimpor `domain`. Tanpa `context`, tanpa jaringan, tanpa `time.Now()`. |
| D4 | Modul aplikasi (`pipeline`, `agent`, `tools`, `session`, `conversation`, …) mengimpor `domain` dan `ports`, **tidak pernah** `adapters/*`. |
| D5 | `adapters/*` mengimpor `ports` dan `domain`; tidak saling mengimpor; tidak mengimpor modul aplikasi. |
| D6 | Tipe pihak ketiga (SDK OpenAI, struct Duffel, tipe library KG) tidak keluar dari paket adapternya. |
| D7 | `transport/http` hanya memanggil modul aplikasi; tidak menyentuh `store` atau `adapters`. |
| D8 | Hanya `app/` dan `cmd/` yang boleh mengimpor semuanya; di situlah perakitan terjadi. |
| D9 | `store/postgres/gen` dan `transport/http/gen` adalah hasil generate; tidak diedit tangan. |
| D10 | `web/` tidak mengandung logika bisnis dan tidak memanggil apa pun selain API Go. |

Aturan D1–D8 ditegakkan otomatis oleh `depguard` di `.golangci.yml`; pelanggaran menggagalkan CI.

## 3. Pemisahan tanggung jawab

| Pertanyaan | Tempatnya |
|---|---|
| Bagaimana bentuk sebuah paket? | `domain` |
| Bagaimana paket dihitung? | `packagebuilder` |
| Dalam urutan apa satu run berjalan? | `pipeline` |
| Bagaimana bicara dengan model? | `agent` + `adapters/openai` |
| Apa yang boleh dilakukan model? | `tools` |
| Bagaimana bicara dengan Duffel? | `adapters/duffel` |
| Bagaimana data disimpan? | `store` |
| Bagaimana bentuk JSON API? | `api/openapi.yaml` + `transport/http` |
| Siapa memakai implementasi yang mana? | `app` |

Ujinya: bila sebuah perubahan supplier menyentuh berkas di luar `adapters/<supplier>` dan `config`, batasnya bocor.

## 4. Konvensi penamaan

### Go

| Hal | Aturan | Contoh |
|---|---|---|
| Paket | Huruf kecil, satu kata, kata benda | `packagebuilder`, `priceindex` |
| Berkas | `snake_case.go`; uji `_test.go` | `search_flights.go` |
| Interface | Kata benda peran; tanpa awalan `I` | `PersonaKnowledge`, `FlightSupplier` |
| Konstruktor | `New<Tipe>` | `NewLoop` |
| Error sentinel | `Err<Hal>` | `ErrOutOfBookingWindow` |
| Konteks | Parameter pertama `ctx context.Context` | — |
| Akronim | Huruf besar semua | `ID`, `URL`, `FX`, `KG`, `SSE` |
| Uji | `Test<Fungsi>_<Skenario>` | `TestBuild_OverBudget_ReturnsAlternatives` |

### Database

`snake_case`; tabel jamak; indeks `<tabel>_<kolom>_idx`; constraint `<tabel>_<hal>_fk|_check|_key`.

### API

Path jamak dengan tanda hubung (`/trip-state`); field `snake_case`; kode error `snake_case`; event SSE `kata.kata`.

### Tool agen

`snake_case`, kata kerja di depan: `search_flights`, `build_packages`.

### Frontend

| Hal | Aturan |
|---|---|
| Komponen | `PascalCase.tsx` |
| Hook | `useCamelCase.ts` |
| Folder fitur | `kebab-case` |
| Tipe API | Hanya dari hasil generate; tidak ditulis ulang |

### Bahasa

Kode, komentar, nama, pesan commit, dan log dalam **bahasa Inggris**. Teks yang dilihat pengguna dalam bahasa Indonesia lewat kamus i18n. Dokumen di `/docs` dalam bahasa Indonesia.

## 5. Konfigurasi

Semua konfigurasi lewat variabel lingkungan, dimuat ke satu struct di `internal/config`. Kelompok:

| Awalan | Isi |
|---|---|
| `APP_` | Lingkungan, port, URL publik |
| `DB_`, `REDIS_` | Koneksi |
| `OPENAI_`, `JEV_` | Kunci, base URL, timeout |
| `MODEL_` | Model per peran dan cadangannya (`MODEL_PLANNER`, `MODEL_EXTRACTOR`, …) |
| `LIMIT_` | Batas run: langkah, kuota tool, biaya, waktu |
| `POLICY_` | Penyangga, definisi tingkat |
| `DUFFEL_`, `LITEAPI_`, `VIATOR_`, `FX_` | Supplier |
| `PERSONA_` | Lokasi berkas pack, jumlah unit per query, ambang celah, reranker |
| `MEMORY_` | URL layanan `openpersona serve`, anggaran token, hidup/mati |
| `GUARDRAILS_` | Lokasi kebijakan dan katalog tool, `GUARDRAILS_SECRET` |
| `TRACER_` | Tujuan event (Postgres, AI Lab) |
| `QUOTA_` | Kuota pengguna |

`.env.example` memuat seluruh kunci tanpa nilai rahasia. Aplikasi gagal start bila konfigurasi wajib kosong.

## 6. Kode hasil generate

| Sumber | Alat | Keluaran |
|---|---|---|
| `api/openapi.yaml` | `oapi-codegen` | `internal/transport/http/gen` |
| `api/openapi.yaml` | `openapi-typescript` | `web/src/lib/api/schema.d.ts` |
| `internal/store/postgres/queries/*.sql` + `migrations/` | `sqlc` | `internal/store/postgres/gen` |

`make generate` menjalankan semuanya. CI menjalankan ulang dan gagal bila ada selisih.

## 7. Perintah Makefile

| Perintah | Fungsi |
|---|---|
| `make dev` | Menjalankan Compose + api + worker + web dengan muat ulang |
| `make generate` | Semua generator |
| `make migrate-up` / `migrate-down` | Migrasi |
| `make lint` | golangci-lint + ESLint |
| `make test` | Uji unit |
| `make test-integration` | Uji dengan testcontainers |
| `make eval` | Menjalankan golden set |
| `make build` | Membangun image |

## 8. Tempat menaruh hal baru

| Ingin menambah | Lakukan |
|---|---|
| Supplier baru | Paket baru di `adapters/`, penuhi port, daftarkan di `app/`, tambah kunci konfigurasi |
| Tool agen baru | Berkas baru di `tools/`, daftarkan di registri, tulis deskripsi dan uji, tambah kasus golden |
| Langkah pipeline baru | Fungsi langkah di `pipeline/`, event baru, perbarui `AI_SYSTEM_DESIGN.md` |
| Endpoint baru | `openapi.yaml` → generate → handler → modul aplikasi |
| Tabel baru | Migrasi → query sqlc → generate → perbarui `DATABASE_SCHEMA.md` |
| Model baru untuk satu peran | Ubah konfigurasi; jalankan `make eval` |
| Destinasi baru | Data KG (lewat library KG) + tabel referensi + entri di `/meta` |
