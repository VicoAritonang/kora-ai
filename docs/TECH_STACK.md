# TECH_STACK.md — Teknologi

Dokumen 3 dari 10 · Draft v1 · 14 November 2026

Versi tidak dipaku di dokumen ini. Aturannya: pakai versi stabil terbaru saat proyek dimulai, kunci di `go.mod` / `package-lock.json`, dan perbarui terjadwal.

---

## 1. Ringkasan

| Lapisan | Pilihan |
|---|---|
| Backend | Go |
| Frontend | Next.js + TypeScript (menunggu keputusan; alternatif SvelteKit) |
| Database | PostgreSQL |
| Cache / pub-sub | Redis |
| Antrean job | River (di atas Postgres) |
| AI generatif | OpenAI (gpt-5-mini, gpt-5-nano) |
| AI keputusan | JEV (TypeSafe AI) |
| Supplier | Duffel, LiteAPI, Viator |
| Observability | OpenTelemetry + log terstruktur |
| CI/CD | GitHub Actions + container |

## 2. Backend (Go)

| Kebutuhan | Pilihan | Alasan | Alternatif yang ditolak |
|---|---|---|---|
| HTTP router | `chi` | Kompatibel `net/http`, middleware sederhana, tanpa sihir | Gin/Fiber: konteks sendiri, kurang cocok dengan pustaka standar |
| Kontrak API | OpenAPI 3.1 + `oapi-codegen` | Satu sumber untuk server Go dan tipe TypeScript | Menulis tangan: rawan melenceng |
| Akses Postgres | `pgx` v5 + `sqlc` | SQL eksplisit, tipe aman, tanpa ORM | GORM: query tersembunyi, sulit dioptimalkan |
| Migrasi | `goose` | Berkas SQL polos, naik-turun | — |
| Job queue | `River` | Transaksional dengan data di Postgres; tidak menambah infrastruktur | NATS/Kafka: berlebihan untuk tahap ini |
| Redis | `go-redis` | Standar de facto | — |
| Klien OpenAI | `openai-go` (SDK resmi), Responses API | Dipelihara resmi; streaming dan tool calling bertipe | — |
| Klien JEV | Klien HTTP tipis buatan sendiri | SDK resmi hanya Python/JS | — |
| Kerangka agen | **Tidak ada**; loop buatan sendiri | Guardrails dan AI Lab butuh hook di tiap langkah; framework menyembunyikan loop | Eino, Genkit Go, ADK Go, LangChainGo |
| Validasi | `go-playground/validator` + validasi domain manual | — | — |
| Konfigurasi | Variabel lingkungan ke struct (`envconfig` atau setara) | 12-factor | Viper: terlalu banyak fitur |
| Logging | `log/slog` (pustaka standar) | Terstruktur, tanpa dependensi | — |
| Tracing/metrik | OpenTelemetry SDK | Netral vendor | — |
| Ketahanan HTTP | Timeout per panggilan + retry ber-jitter + circuit breaker (`sony/gobreaker` atau setara) | — | — |
| Uang | Tipe `Money` buatan sendiri (integer unit terkecil + kode mata uang) | Tidak ada floating point untuk uang | `float64` |
| ID | UUIDv7 | Terurut waktu, baik untuk indeks | UUIDv4 |
| Pengujian | `testing` + `testify` + `testcontainers-go` | Uji integrasi terhadap Postgres/Redis sungguhan | Mock database |

### Mengapa tidak memakai kerangka agen

Riset menunjukkan Genkit Go masih menandai API agennya eksperimental, ADK Go dan Eino lebih matang, dan `openai-go` adalah pustaka klien, bukan kerangka. Loop agen kita kecil (lihat `AI_SYSTEM_DESIGN.md` §5) dan harus memancarkan event serta memanggil guardrail di titik yang kita tentukan. Menulisnya sendiri kira-kira beberapa ratus baris dan memberi kendali penuh. Keputusan ini bisa ditinjau ulang bila kebutuhan multi-agen muncul.

## 3. Frontend

| Kebutuhan | Pilihan | Alasan |
|---|---|---|
| Framework | Next.js (App Router) | Ekosistem React dan ketersediaan developer; lihat catatan di bawah |
| Bahasa | TypeScript (strict) | — |
| Styling | Tailwind CSS | Cepat, konsisten |
| Komponen | shadcn/ui (Radix) | Aksesibel, kodenya milik kita |
| Data fetching | TanStack Query | Cache dan status request |
| Tipe API | `openapi-typescript` + klien fetch bertipe | Digenerate dari kontrak yang sama dengan backend |
| Streaming | `EventSource` / fetch stream untuk SSE | Tanpa pustaka tambahan |
| Form | React Hook Form + Zod | — |
| Peta (Fase 2) | MapLibre GL | Tanpa biaya lisensi per muat |
| Uji | Vitest + Testing Library; Playwright untuk end-to-end | — |

**Aturan pemakaian Next.js:** hanya sebagai lapisan tampilan. Tanpa server actions untuk logika bisnis, tanpa akses database, tanpa memanggil AI atau supplier. Semua data lewat API Go.

**Next.js vs SvelteKit:** keduanya sanggup. Next.js dipilih karena rekrutmen dan ekosistem komponen. SvelteKit lebih ringan dan layak dipilih bila developer frontend pertama mahir Svelte. Backend tidak terpengaruh.

## 4. Data

| Kebutuhan | Pilihan | Alasan |
|---|---|---|
| Database utama | PostgreSQL | Relasional, JSONB untuk payload fleksibel, partisi untuk event, antrean job |
| Ekstensi | `citext`, `pgcrypto`; `pgvector` hanya bila library KG membutuhkannya | — |
| Cache, rate limit, pub-sub | Redis | — |
| Object storage (Fase 2) | Kompatibel S3 | Arsip event dan payload supplier mentah |
| Persona dan memori | **OpenPersona** (`github.com/VicoAritonang/open-persona`; module `github.com/openpersona/openpersona`) | Go, SQLite murni tanpa cgo. Persona pack dipakai sebagai library; memori sebagai layanan `openpersona serve`. Perlu `replace` di `go.mod` karena module path berbeda dari lokasi repo. |
| Guardrails | **guard-rails-engine** (`go.vicoworks.com/guard-rails-engine/pkg/guardrails`, MIT) | Go, di dalam proses, empat tahap (inbound, runtime, tool result, outbound). Butuh Go 1.26+. |
| Norma biaya | Tabel `cost_norms` di Postgres | Angka terstruktur untuk penyusun paket |

## 5. AI providers

| Peran | Model | Harga (USD / 1 juta token: input · cached · output) |
|---|---|---|
| Routing, gating, verifikasi rubrik | JEV (`jev-1.13`, versi dipaku) | 0,042 · — · gratis |
| Ekstraksi slot, ringkasan, judul | gpt-5-nano | 0,05 · 0,005 · 0,40 |
| Loop perencana, narasi | gpt-5-mini | 0,25 · 0,025 · 2,00 |
| Riset web | gpt-5-mini + web search tool | + $10 per 1.000 panggilan |
| Embedding (bila diperlukan KG) | text-embedding-3-small | 0,02 |

Harga diambil dari halaman resmi OpenAI dan listing JEV pada 14 November 2026.

Catatan:

- **gpt-4o-mini tidak dipakai.** gpt-5-nano lebih murah di semua komponen harga. Latensinya perlu diukur karena ia model reasoning.
- **Kandidat untuk dibandingkan di AI Lab:** gpt-6-luna (0,10 · 0,01 · 0,50), gpt-5.6-luna, gpt-6.1-terra. Belum ada data kualitas untuk kasus kita.
- **JEV:** konteks 32 ribu token; hanya teks; batas laju terpublikasi 1.200 request/menit; rentan terhadap teks injeksi, jadi tidak menjadi pengaman tunggal.
- Nama model per peran ada di konfigurasi (`MODEL_PLANNER`, `MODEL_EXTRACTOR`, dst.), bukan di kode.
- **Jev ≠ Jev Router.** Jev adalah model keputusan. Jev Router (`typesafe/jev-router` di OpenRouter, default chat model OpenPersona) adalah router ke model generatif dengan harga mengikuti model terpilih. Untuk planner dan narator kita memakai model yang dipaku agar evaluasi bisa diulang; Jev Router hanya kandidat pembanding.
- OpenPersona memakai `gpt-4o-mini` sebagai extractor default (training pack dan memori). Diganti lewat `OPENPERSONA_EXTRACT_MODEL` setelah dibandingkan.

## 6. Supplier dan data eksternal

| Kebutuhan | Pilihan | Model biaya | Status |
|---|---|---|---|
| Penerbangan | Duffel | $3/order + 1% nilai order; pencarian berlebih $0,005 di atas rasio 1500:1; FX 2% | Perlu spike: cakupan LCC, rute CGK–KIX, IDR |
| Penginapan | LiteAPI (Nuitée) | Komisi lewat `margin` | Perlu spike: latensi, cakupan Osaka |
| Aktivitas | Viator Partner API (Basic) | Afiliasi | Fase 2 |
| Aktivitas (Asia) | Klook | Afiliasi / kemitraan | Fase 2, perlu persetujuan |
| Kurs | Satu penyedia kurs harian | — | Dipilih di Fase 0 |
| Cadangan harga penerbangan | SerpApi (Google Flights) | Per pencarian | Hanya bila Duffel tidak memadai; ada risiko ToS |

Tidak tersedia: Amadeus Self-Service (ditutup 17 Juli 2026), Kiwi Tequila (hanya undangan), Booking.com Demand API (butuh kontrak Managed Affiliate).

## 7. Infrastruktur

| Kebutuhan | Usulan | Catatan |
|---|---|---|
| Kemasan | Docker (image distroless untuk Go) | — |
| Lokal | Docker Compose | Postgres, Redis, api, worker, web |
| Hosting | Layanan container terkelola + Postgres terkelola + Redis terkelola | Usulan: AWS (ECS Fargate, RDS, ElastiCache) di region Jakarta. Penyedia lain setara boleh; tidak ada ketergantungan khusus di kode. |
| Infrastructure as code | Terraform atau CDK | Dipilih saat tim infra ditentukan |
| Rahasia | Secret manager penyedia cloud | — |
| CDN/WAF | Di depan web dan api | Rate limit lapis pertama |
| CI/CD | GitHub Actions | Lint, uji, build image, migrasi, deploy |
| Pelacakan error | Sentry atau setara | — |
| Backend observability | Kolektor OpenTelemetry → backend pilihan | — |

Untuk tahap prebuild, staging boleh berupa satu VM dengan Docker Compose. Produksi memakai layanan terkelola.

## 8. Perkakas pengembangan

| Kebutuhan | Pilihan |
|---|---|
| Lint Go | `golangci-lint` |
| Format Go | `gofumpt` + `goimports` |
| Aturan impor antar-modul | `depguard` (bagian dari golangci-lint) |
| Lint/format web | ESLint + Prettier |
| Task runner | `Makefile` (atau `task`) |
| Pre-commit | `lefthook` |
| Generator | `sqlc`, `oapi-codegen`, `openapi-typescript` |
| Evaluasi AI | Runner golden set di `/eval`, keluaran ke AI Lab |

## 9. Yang sengaja tidak dipakai

| Teknologi | Alasan |
|---|---|
| Microservices, service mesh, Kubernetes | Beban operasional tidak sebanding untuk tim awal |
| Kafka / message broker terpisah | River + Redis pub-sub cukup |
| ORM | SQL eksplisit lebih mudah diaudit dan dioptimalkan |
| GraphQL | Satu klien, kontrak REST + SSE lebih sederhana |
| WebSocket | Streaming satu arah; SSE cukup dan lebih mudah disambung ulang |
| Vector database terpisah | Retrieval ditangani library KG |
| Kerangka agen | Lihat §2 |
