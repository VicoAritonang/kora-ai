# API_SPEC.md — Kontrak API

Dokumen 5 dari 10 · Draft v1 · 14 November 2026

> Implementasi: kontrak mesin ada di `api/openapi.yaml`. Tambahan aditif: event SSE `destinations.ready`, nilai `feasibility` `unbounded`, field `memory_available` di `GET /me`.

Dokumen ini adalah ringkasan yang bisa dibaca manusia. Sumber kebenaran saat implementasi adalah `api/openapi.yaml`; keduanya harus sinkron.

---

## 1. Dasar

| Hal | Nilai |
|---|---|
| Base URL | `/api/v1` |
| Format | JSON (`application/json; charset=utf-8`); streaming `text/event-stream` |
| Penamaan field | `snake_case` |
| Waktu | RFC 3339, UTC |
| ID | UUID |
| Uang | Objek `{ "amount": 3250000, "currency": "IDR" }`, `amount` integer dalam unit terkecil |
| Paginasi | Kursor: `?limit=20&cursor=...` → `{ "items": [...], "next_cursor": "..." }` |

## 2. Autentikasi

- Sesi berbasis cookie: `sid`, `HttpOnly`, `Secure`, `SameSite=Lax`.
- Pengguna tanpa cookie mendapat sesi **tamu** lewat `POST /auth/guest` (dipanggil otomatis oleh web).
- Login memakai magic link email. Saat tamu login, percakapannya dipindahkan ke akun.
- Request yang mengubah data wajib menyertakan header `X-CSRF-Token` (nilai dari `GET /me`).
- Endpoint internal (`/internal/v1`) memakai `Authorization: Bearer <service-token>` dan tidak diekspos ke publik.

## 3. Header umum

| Header | Arah | Fungsi |
|---|---|---|
| `X-Request-Id` | Keduanya | Korelasi log; dibuat server bila tidak ada |
| `Idempotency-Key` | Request | Wajib untuk `POST /conversations/{id}/messages`; menjamin satu pesan = satu run |
| `X-CSRF-Token` | Request | Proteksi CSRF |
| `Last-Event-ID` | Request | Menyambung ulang SSE |
| `Retry-After` | Response | Pada `429` dan `503` |
| `X-RateLimit-Remaining` | Response | Sisa kuota run hari ini |

## 4. Format error

Mengikuti RFC 9457 (`application/problem+json`):

```json
{
  "type": "https://docs.example/errors/validation_failed",
  "title": "Validasi gagal",
  "status": 422,
  "code": "validation_failed",
  "detail": "Field 'content' wajib diisi.",
  "request_id": "0193...",
  "errors": [{ "field": "content", "code": "required" }]
}
```

| HTTP | `code` | Arti |
|---|---|---|
| 400 | `bad_request` | Bentuk request salah |
| 401 | `unauthenticated` | Tidak ada sesi yang sah |
| 403 | `forbidden` | Bukan pemilik sumber daya; CSRF gagal |
| 404 | `not_found` | — |
| 409 | `run_in_progress` | Percakapan masih punya run aktif |
| 409 | `idempotency_conflict` | Kunci sama, isi request berbeda |
| 409 | `state_conflict` | Versi state usang |
| 410 | `offer_expired` | Offer sudah kedaluwarsa; minta cek ulang harga |
| 422 | `validation_failed` | Validasi isi gagal |
| 429 | `rate_limited` | Kuota atau laju terlampaui |
| 429 | `capacity_exhausted` | Server sedang penuh; coba lagi |
| 500 | `internal_error` | — |
| 502 | `upstream_error` | Penyedia eksternal gagal |
| 503 | `unavailable` | Pemeliharaan atau ketergantungan inti mati |

Kode error **di dalam run** (dikirim lewat SSE, bukan status HTTP): `blocked_by_guardrail`, `model_unavailable`, `kg_unavailable`, `budget_exceeded` (batas biaya run), `step_limit_reached`, `timeout`, `interrupted`, `cancelled`.

## 5. Endpoint

### 5.1 Auth dan profil

| Method | Path | Fungsi |
|---|---|---|
| POST | `/auth/guest` | Membuat sesi tamu |
| POST | `/auth/magic-link` | Mengirim tautan login ke email |
| POST | `/auth/magic-link/verify` | Menukar token dengan sesi |
| POST | `/auth/logout` | Mencabut sesi |
| GET | `/me` | Profil + token CSRF + kuota |
| PATCH | `/me` | Mengubah profil |
| DELETE | `/me` | Meminta penghapusan akun |
| GET | `/me/export` | Mengekspor data pengguna |

`POST /auth/magic-link`

```json
{ "email": "nama@contoh.com" }
```

Selalu `202`, apa pun emailnya (tidak membocorkan keberadaan akun).

`GET /me` → `200`

```json
{
  "id": "0193...",
  "is_guest": false,
  "email": "nama@contoh.com",
  "display_name": "Rina",
  "home_city_code": "JKT",
  "locale": "id",
  "memory_enabled": true,
  "csrf_token": "...",
  "quota": { "runs_per_day": 30, "runs_remaining": 27 }
}
```

`PATCH /me` menerima: `display_name`, `home_city_code`, `locale`, `memory_enabled`.

### 5.2 Percakapan

| Method | Path | Fungsi |
|---|---|---|
| GET | `/conversations` | Daftar percakapan |
| POST | `/conversations` | Membuat percakapan |
| GET | `/conversations/{id}` | Detail + TripState + run aktif |
| PATCH | `/conversations/{id}` | Mengubah judul |
| DELETE | `/conversations/{id}` | Menghapus |
| GET | `/conversations/{id}/messages` | Riwayat pesan |
| POST | `/conversations/{id}/messages` | Mengirim pesan dan memulai run |
| PATCH | `/conversations/{id}/trip-state` | Mengoreksi slot/asumsi secara langsung |

`GET /conversations/{id}` → `200`

```json
{
  "id": "0193...",
  "title": "Osaka musim gugur",
  "trip_state": {
    "version": 4,
    "origin": { "code": "JKT", "label": "Jakarta" },
    "destination": { "code": "OSA", "label": "Osaka" },
    "budget": { "amount": 20000000, "currency": "IDR" },
    "date_window": { "season": "autumn", "year": 2027 },
    "nights": 6,
    "pax": { "adults": 1, "children": 0 },
    "interests": ["festival"],
    "assumptions": [
      { "slot": "nights", "value": 6, "reason": "Durasi umum untuk Osaka dengan budget ini" }
    ]
  },
  "active_run_id": null,
  "selected_package_id": null,
  "created_at": "2026-11-14T03:00:00Z"
}
```

`POST /conversations/{id}/messages`

```json
{ "content": "Saya orang Jakarta, ada uang 20 juta, ingin ke Osaka musim gugur nanti..." }
```

→ `202`

```json
{
  "message": { "id": "0193...", "seq": 1, "role": "user", "content": "...", "created_at": "..." },
  "run": { "id": "0193...", "status": "running", "stream_url": "/api/v1/runs/0193.../stream" }
}
```

Validasi: `content` 1–4.000 karakter. `409 run_in_progress` bila masih ada run aktif.

`PATCH /conversations/{id}/trip-state`

```json
{ "version": 4, "set": { "nights": 5, "pax": { "adults": 2 } } }
```

→ `200` dengan TripState baru. Tidak memulai run; klien mengirim pesan berikutnya atau memanggil susun ulang lewat pesan. `409 state_conflict` bila `version` usang.

### 5.3 Run

| Method | Path | Fungsi |
|---|---|---|
| GET | `/runs/{id}` | Status run |
| GET | `/runs/{id}/stream` | Stream SSE |
| POST | `/runs/{id}/cancel` | Membatalkan |

`GET /runs/{id}` → `200`

```json
{
  "id": "0193...",
  "conversation_id": "0193...",
  "status": "completed",
  "mode": "plan_trip",
  "started_at": "...",
  "finished_at": "...",
  "error_code": null
}
```

### 5.4 Paket

| Method | Path | Fungsi |
|---|---|---|
| GET | `/conversations/{id}/packages` | Paket milik percakapan (terbaru dulu) |
| GET | `/packages/{id}` | Detail paket lengkap |
| POST | `/packages/{id}/select` | Menandai paket terpilih |
| POST | `/packages/{id}/reprice` | Cek ulang harga item `live` |
| POST | `/packages/{id}/items/{item_id}/outbound` | Mencatat klik dan mengembalikan tautan keluar |

`GET /packages/{id}` → `200`

```json
{
  "id": "0193...",
  "tier": "seimbang",
  "title": "Osaka 6 malam — Seimbang",
  "status": "proposed",
  "feasibility": "fits",
  "budget": { "amount": 20000000, "currency": "IDR" },
  "total": { "amount": 17850000, "currency": "IDR" },
  "breakdown": {
    "live": { "amount": 12400000, "currency": "IDR" },
    "estimate": { "amount": 5450000, "currency": "IDR" },
    "buffer": { "amount": 2000000, "currency": "IDR" },
    "remaining": { "amount": 150000, "currency": "IDR" },
    "shortfall": { "amount": 0, "currency": "IDR" }
  },
  "assumptions": [ { "slot": "nights", "value": 6, "reason": "..." } ],
  "alternatives": [],
  "days": [
    { "day_index": 0, "date": "2027-10-10", "title": "Tiba di Osaka", "summary": "..." }
  ],
  "items": [
    {
      "id": "0193...",
      "day_index": null,
      "category": "flight",
      "title": "Jakarta (CGK) → Osaka (KIX), pulang-pergi",
      "quantity": 1,
      "unit_price": { "amount": 7200000, "currency": "IDR" },
      "amount_idr": { "amount": 7200000, "currency": "IDR" },
      "price_kind": "live",
      "source": { "label": "Duffel", "url": null },
      "priced_at": "2026-11-14T03:01:12Z",
      "expires_at": "2026-11-14T03:31:12Z",
      "has_outbound": true,
      "details": { "segments": [], "baggage": "..." }
    },
    {
      "id": "0193...",
      "day_index": 1,
      "category": "meal",
      "title": "Makan (3 kali)",
      "quantity": 3,
      "unit_price": { "amount": 1200, "currency": "JPY" },
      "amount_idr": { "amount": 390000, "currency": "IDR" },
      "price_kind": "estimate",
      "source": { "label": "Acuan biaya makan Osaka, tingkat sedang", "url": null },
      "priced_at": "2026-10-01T00:00:00Z",
      "expires_at": null,
      "has_outbound": false,
      "details": {}
    }
  ],
  "created_at": "..."
}
```

Angka pada contoh adalah ilustrasi bentuk data, bukan harga nyata.

`POST /packages/{id}/reprice` → `200`

```json
{
  "package_id": "0193...",
  "new_package_id": "0194...",
  "changes": [
    {
      "item_id": "0193...",
      "previous": { "amount": 7200000, "currency": "IDR" },
      "current": { "amount": 7450000, "currency": "IDR" },
      "status": "changed"
    }
  ],
  "total_previous": { "amount": 17850000, "currency": "IDR" },
  "total_current": { "amount": 18100000, "currency": "IDR" },
  "feasibility": "fits"
}
```

`status` per item: `unchanged`, `changed`, `unavailable`. Cek ulang membuat paket baru; paket lama menjadi `stale`. Dibatasi lajunya per paket.

`POST /packages/{id}/items/{item_id}/outbound` → `200 { "url": "https://..." }` atau `410 offer_expired`.

### 5.5 Memori, pengingat, penilaian

| Method | Path | Fungsi |
|---|---|---|
| GET | `/me/memory` | Daftar fakta yang diingat tentang pengguna |
| DELETE | `/me/memory/{id}` | Menghapus satu fakta (menunggu fitur hapus di OpenPersona) |
| DELETE | `/me/memory` | Menghapus semua (menunggu penghapusan scope di OpenPersona) |
| GET | `/reminders` | Daftar pengingat |
| POST | `/reminders` | Membuat pengingat |
| DELETE | `/reminders/{id}` | Membatalkan |
| POST | `/feedback` | Penilaian pesan atau paket |

`POST /reminders` (hanya pengguna terdaftar)

```json
{ "conversation_id": "0193...", "kind": "booking_window_open" }
```

`POST /feedback`

```json
{ "package_id": "0193...", "rating": -1, "reason": "price_wrong", "comment": "Harga hotel beda di situsnya" }
```

`reason`: `price_wrong`, `not_relevant`, `too_expensive`, `missing_info`, `other`.

### 5.6 Sistem

| Method | Path | Fungsi |
|---|---|---|
| GET | `/healthz` | Proses hidup |
| GET | `/readyz` | Postgres dan Redis terjangkau |
| GET | `/meta` | Destinasi yang didukung, kota asal, batas, versi |

### 5.7 Internal (AI Lab)

| Method | Path | Fungsi |
|---|---|---|
| GET | `/internal/v1/runs` | Daftar run dengan filter waktu/status/mode |
| GET | `/internal/v1/runs/{id}` | Run + snapshot konfigurasi |
| GET | `/internal/v1/runs/{id}/events` | Seluruh event (termasuk `internal`) |
| GET | `/internal/v1/runs/{id}/artifacts/{artifact_id}` | Hasil tool lengkap |

Bila AI Lab lebih cocok menerima dorongan event, adapter `Tracer` yang mengirimkannya; endpoint ini tetap ada untuk pembacaan ulang.

## 6. Stream SSE

`GET /runs/{id}/stream` → `text/event-stream`

Setiap event:

```
id: 17
event: status
data: {"phase":"searching_flights","label":"Mencari penerbangan Jakarta–Osaka"}

```

- `id` adalah `seq` event dalam run. Sambung ulang dengan `Last-Event-ID`; server memutar ulang event setelah ID itu.
- Komentar `: ping` dikirim tiap 15 detik.
- Stream ditutup server setelah `run.completed` atau `run.failed`.
- Menyambung ke run yang sudah selesai memutar ulang seluruh event lalu menutup.

| `event` | `data` | Keterangan |
|---|---|---|
| `run.started` | `{ run_id, mode }` | — |
| `status` | `{ phase, label }` | Progres yang ditampilkan ke pengguna |
| `trip_state.updated` | `{ trip_state }` | Slot hasil ekstraksi |
| `assumptions` | `{ items: [{ slot, value, reason }] }` | Asumsi yang dipakai |
| `clarification` | `{ question, options? }` | Sistem bertanya; run selesai setelah ini |
| `tool.started` | `{ tool, label }` | Label ramah pengguna, tanpa argumen mentah |
| `tool.finished` | `{ tool, ok, summary }` | — |
| `packages.ready` | `{ packages: [{ id, tier, title, total, feasibility }] }` | Klien memuat detail lewat `GET /packages/{id}` |
| `message.delta` | `{ text }` | Potongan narasi |
| `message.completed` | `{ message_id }` | — |
| `notice` | `{ code, text }` | Mis. supplier gagal dan item diestimasi |
| `run.completed` | `{ run_id }` | Akhir sukses |
| `run.failed` | `{ run_id, code, message, retryable }` | Akhir gagal |

Nilai `phase`: `understanding`, `retrieving_knowledge`, `searching_flights`, `searching_stays`, `searching_activities`, `researching_web`, `building_packages`, `verifying`, `writing`.

Klien harus mengabaikan jenis event yang tidak dikenalnya.

## 7. Validasi

| Field | Aturan |
|---|---|
| `content` pesan | 1–4.000 karakter setelah trim |
| `email` | Format email; maks. 254 karakter |
| `home_city_code` | Harus ada di `/meta.origins` |
| `nights` | 1–60 |
| `pax.adults` | 1–9; `pax.children` 0–8; total ≤ 9 |
| `budget.amount` | > 0; `currency` ada di daftar yang didukung |
| `limit` | 1–100, default 20 |

Validasi bentuk dilakukan di lapisan transport dari skema OpenAPI; validasi domain di modul masing-masing. Error validasi domain juga dikembalikan sebagai `422`.

## 8. Rate limit

| Lingkup | Batas usulan |
|---|---|
| Run per hari, tamu | 10 |
| Run per hari, terdaftar | 30 |
| Run bersamaan per pengguna | 1 per percakapan, 2 total |
| Request non-run per menit per sesi | 120 |
| `POST /auth/magic-link` per email per jam | 5 |
| `POST /packages/{id}/reprice` | 1 per 5 menit per paket |
| Per IP (tamu) | Batas tambahan untuk mencegah pembuatan sesi tamu massal |

## 9. Versi

- Versi mayor di path (`/v1`). Perubahan yang merusak membuat `/v2`; `/v1` dipertahankan minimal 6 bulan.
- Perubahan aditif (field baru, jenis event SSE baru, nilai enum baru di response) **bukan** perubahan merusak; klien wajib toleran.
- `GET /meta` mengembalikan `api_version` dan `build`.
- Setiap perubahan `openapi.yaml` diperiksa di CI dengan pendeteksi perubahan merusak.
