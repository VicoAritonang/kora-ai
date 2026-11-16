# Kora AI — agen perjalanan berbasis budget

Agen AI yang menjawab *"mau ke X, butuh berapa?"* dan *"punya Rp Y, bisa ke mana?"* dengan paket **Hemat / Seimbang / Nyaman** yang dirinci per item, dengan label **live** (dari supplier) atau **estimasi** (dari acuan biaya). LLM tidak pernah menulis atau menghitung harga; semua angka disusun kode Go yang deterministik.

Rencana teknis: [`docs/`](docs/) (mulai dari [TECHNICAL_SPEC](docs/TECHNICAL_SPEC.md)). Penyimpangan dan temuan implementasi: [IMPLEMENTATION_NOTES](docs/IMPLEMENTATION_NOTES.md).

## Arsitektur singkat

```
web (Next.js) ──HTTPS+SSE──► api (Go) ──► pipeline: guard-in → route (JEV) → extract (gpt-5-nano)
                                         → clarify → retrieve (OpenPersona) → prefetch → agent loop (gpt-5-mini + tools)
                                         → packagebuilder (kode) → invarian → narasi per paragraf (+ guard-out) → rubrik (JEV)
                               worker (Go, River) ──► indeks biaya, ringkasan, pengingat, retensi, sapu run
Postgres (Neon) · Redis · guard-rails-engine (in-process) · OpenPersona pack (in-process)
```

## Menjalankan

Prasyarat: Docker, Node 20+. Go **tidak** perlu terpasang: `scripts/go.sh` menjalankan toolchain di container.

1. **Database (Neon).** Terapkan skema lalu data acuan, sekali, dari terminal:
   ```sh
   psql "$NEON_DB_URL" -f db/schema.sql
   psql "$NEON_DB_URL" -f db/seed.sql
   ```
   Atau tempel isi kedua berkas di Neon SQL Editor. Tanpa psql lokal: `scripts/psql.sh -f /db/schema.sql` lalu `scripts/psql.sh -f /db/seed.sql` (membaca `NEON_DB_URL` dari `.env`).
2. **Konfigurasi.** `.env` minimal: `OPENAI_API_KEY`, `OPENROUTER_API_KEY` (JEV), `NEON_DB_URL`. Semua kunci lain ada di [`.env.example`](.env.example) dengan default aman.
3. **Backend.** `docker compose up --build -d redis api worker` → api di http://localhost:8080. Worker mengisi Indeks Biaya Destinasi saat start.
4. **Frontend.** `cd web && npm install && npm run dev` → http://localhost:3000. Atau `docker compose up --build web`.
5. Login magic link di mode lokal: tautan dicetak di log api (`docker compose logs api | grep "email"`).
6. **Opsional — Agent Reliability Lab** (inspeksi setiap run saat development). Jalankan Lab dari repo-nya (`docker compose up -d` → http://localhost:8000), lalu di `.env` Kora isi `STAGE=dev` dan `LAB_WS_URL=ws://host.docker.internal:8000/api/agent-lab/ws` (atau `ws://localhost:8000/...` bila api dijalankan di host), dan restart api. Hanya diizinkan dengan `APP_ENV=local`. Detail pemetaan: [IMPLEMENTATION_NOTES §10](docs/IMPLEMENTATION_NOTES.md).

## Perintah

| Perintah | Fungsi |
|---|---|
| `scripts/go.sh test ./...` | Uji unit Go |
| `scripts/test-integration.sh` | Uji store terhadap Postgres sementara di Docker |
| `scripts/go.sh run ./cmd/eval` | Golden set (`eval/golden`) dengan model asli, laporan di `eval/reports` |
| `scripts/go.sh run ./cmd/worker refresh-index` | Isi ulang Indeks Biaya Destinasi |
| `scripts/go.sh run ./cmd/worker import-norms data/norms/osaka.json` | Impor norma biaya hasil kurasi |
| `scripts/train-persona.sh` | Latih ulang `persona/packs/kora.persona` dari `persona/sources` |
| `node scripts/build-schema.mjs` | Regenerasi `db/schema.sql`, `db/seed.sql`, migrasi goose |
| `cd web && npm run generate` | Regenerasi tipe TypeScript dari `api/openapi.yaml` |

## Status data

- **Supplier**: `sandbox` (harga simulasi deterministik) sampai spike Duffel/LiteAPI selesai. Adapter Duffel dan LiteAPI sudah ada; aktifkan dengan `SUPPLIER_FLIGHTS=duffel` + `DUFFEL_ACCESS_TOKEN`, `SUPPLIER_STAYS=liteapi` + `LITEAPI_API_KEY`.
- **Norma biaya dan persona pack**: draf placeholder, wajib ditinjau kurator travel agent (IMPLEMENTATION_PLAN F0-7). UI menampilkan labelnya.
- **Memori pengguna**: `MEMORY_ENABLED=false` sampai OpenPersona mendukung penghapusan scope (UU PDP).

## Struktur

Lihat [PROJECT_STRUCTURE](docs/PROJECT_STRUCTURE.md). Batas modul D1–D8 ditegakkan `depguard` di [`.golangci.yml`](.golangci.yml).
