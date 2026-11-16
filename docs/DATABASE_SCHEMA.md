# DATABASE_SCHEMA.md — Desain Database

Dokumen 4 dari 10 · Draft v1 · 14 November 2026

Database: PostgreSQL. Knowledge graph **tidak** disimpan di skema ini; ia dikelola library pemilik produk.

> Implementasi: skema yang berlaku ada di `db/schema.app.sql` (diterapkan lewat `db/schema.sql`). Tambahan terhadap dokumen ini ditandai `[impl]` di berkas itu dan dirangkum di [IMPLEMENTATION_NOTES](IMPLEMENTATION_NOTES.md) §2.

---

## 1. Konvensi

| Hal | Aturan |
|---|---|
| Nama | `snake_case`; tabel jamak; kunci asing `<entitas>_id` |
| Primary key | `id uuid` (UUIDv7, dibuat aplikasi), kecuali tabel event (`bigint identity`) |
| Waktu | `timestamptz`, UTC; setiap tabel punya `created_at`; tabel yang bisa berubah punya `updated_at` |
| Uang | `bigint` dalam unit terkecil mata uang (ISO 4217) + kolom `*_currency char(3)`. IDR dan JPY tanpa pecahan. Tidak ada `float`/`numeric` untuk jumlah uang. |
| Kurs | `numeric(18,8)` |
| Enum | Tipe `enum` Postgres untuk himpunan kecil dan stabil; `text` + `check` untuk yang sering bertambah |
| JSON | `jsonb` hanya untuk payload yang bentuknya milik pihak luar atau memang fleksibel |
| Hapus | Hard delete dengan `on delete cascade` untuk data milik pengguna; tidak ada soft delete kecuali `users.deleted_at` selama masa tenggang |

## 2. Diagram relasi

```
users ─┬─< auth_identities
       ├─< auth_sessions
       ├─< conversations ─┬─< messages
       │                  ├─< runs ─┬─< run_events
       │                  │         ├─< tool_artifacts
       │                  │         └─< trip_requests ─< packages ─┬─< package_days
       │                  │                                        └─< package_items >─ offers
       │                  └─< packages
       ├─< reminders
       ├─< feedback
       └─< outbound_clicks >─ package_items

offers          (snapshot supplier, tidak dimiliki pengguna)
price_cache     (Indeks Biaya Destinasi)
fx_rates
idempotency_keys
river_*         (dikelola River)
```

## 3. Tipe enum

```sql
create type message_role   as enum ('user', 'assistant', 'notice');
create type run_status     as enum ('running', 'completed', 'failed', 'cancelled', 'blocked');
create type run_mode       as enum ('plan_trip', 'budget_to_destination', 'refine', 'info', 'chitchat', 'out_of_scope');
create type offer_kind     as enum ('flight', 'stay', 'activity');
create type package_tier   as enum ('hemat', 'seimbang', 'nyaman');
create type package_status as enum ('proposed', 'selected', 'stale');
create type feasibility    as enum ('fits', 'tight', 'over_budget');
create type item_category  as enum ('flight', 'stay', 'activity', 'meal', 'local_transport', 'visa', 'insurance', 'other');
create type price_kind     as enum ('live', 'estimate');
create type event_visibility as enum ('public', 'internal');
```

## 4. Tabel

### 4.1 Identitas

```sql
create table users (
    id              uuid primary key,
    email           citext unique,                 -- null untuk tamu
    display_name    text,
    is_guest        boolean not null default true,
    home_city_code  text,                          -- kode kota/bandara asal default
    locale          text not null default 'id',
    memory_enabled  boolean not null default false, -- persetujuan memori lintas sesi
    created_at      timestamptz not null default now(),
    updated_at      timestamptz not null default now(),
    last_seen_at    timestamptz,
    deleted_at      timestamptz
);

create table auth_identities (
    id               uuid primary key,
    user_id          uuid not null references users(id) on delete cascade,
    provider         text not null check (provider in ('magic_link', 'google')),
    provider_subject text not null,
    created_at       timestamptz not null default now(),
    unique (provider, provider_subject)
);

create table auth_sessions (
    id          uuid primary key,
    user_id     uuid not null references users(id) on delete cascade,
    token_hash  bytea not null unique,             -- SHA-256 dari token cookie
    user_agent  text,
    ip_hash     bytea,
    created_at  timestamptz not null default now(),
    expires_at  timestamptz not null,
    revoked_at  timestamptz
);
create index auth_sessions_user_idx on auth_sessions (user_id);

create table magic_link_tokens (
    id          uuid primary key,
    email       citext not null,
    token_hash  bytea not null unique,
    expires_at  timestamptz not null,
    used_at     timestamptz,
    created_at  timestamptz not null default now()
);
```

Saat tamu login, `users` tamu digabung ke akun: seluruh `conversations` dipindahkan, baris tamu dihapus.

### 4.2 Percakapan

```sql
create table conversations (
    id                    uuid primary key,
    user_id               uuid not null references users(id) on delete cascade,
    title                 text,
    trip_state            jsonb not null default '{}',   -- TripState, lihat MODULE_SPEC
    trip_state_version    integer not null default 0,    -- optimistic locking
    summary               text,                          -- ringkasan giliran lama
    summary_upto_seq      bigint not null default 0,
    selected_package_id   uuid,                          -- FK ditambahkan setelah packages
    guard_session         jsonb,                         -- Session guard-rails-engine; tidak pernah dari klien
    created_at            timestamptz not null default now(),
    updated_at            timestamptz not null default now(),
    last_message_at       timestamptz
);
create index conversations_user_recent_idx on conversations (user_id, last_message_at desc);

create table messages (
    id               uuid primary key,
    conversation_id  uuid not null references conversations(id) on delete cascade,
    seq              bigint not null,                    -- urutan dalam percakapan
    role             message_role not null,
    content          text not null,
    blocks           jsonb,                              -- referensi paket, asumsi, klarifikasi
    run_id           uuid,                               -- run yang menghasilkan (asisten) atau dipicu (user)
    created_at       timestamptz not null default now(),
    unique (conversation_id, seq)
);
```

### 4.3 Run dan event

```sql
create table runs (
    id                  uuid primary key,
    conversation_id     uuid not null references conversations(id) on delete cascade,
    user_message_id     uuid not null references messages(id) on delete cascade,
    status              run_status not null default 'running',
    mode                run_mode,
    step_count          integer not null default 0,
    input_tokens        bigint not null default 0,
    cached_tokens       bigint not null default 0,
    output_tokens       bigint not null default 0,
    cost_usd_micros     bigint not null default 0,       -- 1 USD = 1.000.000
    flags               text[] not null default '{}',    -- mis. verification_skipped
    error_code          text,
    config_snapshot     jsonb not null,                  -- model per peran, versi prompt, batas,
                                                         -- versi persona pack, versi kebijakan guardrails
    started_at          timestamptz not null default now(),
    finished_at         timestamptz
);
create index runs_conversation_idx on runs (conversation_id, started_at desc);
create unique index runs_one_active_per_conversation
    on runs (conversation_id) where status = 'running';
create index runs_running_idx on runs (started_at) where status = 'running';

create table run_events (
    id               bigint generated always as identity,
    run_id           uuid not null,
    seq              integer not null,                   -- urutan dalam run
    type             text not null,                      -- lihat AI_SYSTEM_DESIGN §11
    visibility       event_visibility not null default 'internal',
    step             text,                               -- langkah pipeline
    payload          jsonb not null,
    model            text,
    input_tokens     integer,
    cached_tokens    integer,
    output_tokens    integer,
    cost_usd_micros  bigint,
    latency_ms       integer,
    created_at       timestamptz not null default now(),
    primary key (id, created_at)
) partition by range (created_at);
create index run_events_run_seq_idx on run_events (run_id, seq);
-- Partisi bulanan dibuat oleh job worker.

create table tool_artifacts (
    id          uuid primary key,
    run_id      uuid not null references runs(id) on delete cascade,
    tool_name   text not null,
    args        jsonb not null,
    result      jsonb not null,                          -- hasil lengkap (di luar konteks model)
    digest      text not null,                           -- ringkasan yang masuk konteks
    created_at  timestamptz not null default now()
);
create index tool_artifacts_run_idx on tool_artifacts (run_id);
```

`run_events` tidak memakai FK ke `runs` karena dipartisi dan dihapus per partisi; konsistensi dijaga aplikasi dan job retensi.

### 4.4 Permintaan perjalanan

```sql
create table trip_requests (
    id                 uuid primary key,
    conversation_id    uuid not null references conversations(id) on delete cascade,
    run_id             uuid not null references runs(id) on delete cascade,
    origin_code        text,
    destination_code   text,                             -- null pada mode budget → tujuan
    budget_amount      bigint,
    budget_currency    char(3),
    budget_idr         bigint,
    date_from          date,
    date_to            date,
    date_window        jsonb,                            -- mis. {"season":"autumn","year":2027}
    nights             integer check (nights between 1 and 60),
    pax_adults         integer not null default 1 check (pax_adults between 1 and 9),
    pax_children       integer not null default 0 check (pax_children between 0 and 8),
    interests          text[] not null default '{}',
    special_requests   jsonb not null default '[]',
    assumptions        jsonb not null default '[]',      -- slot yang diisi sistem + alasannya
    created_at         timestamptz not null default now(),
    check (budget_amount is not null or destination_code is not null)
);
```

Baris ini adalah **snapshot tak berubah** dari slot yang dipakai satu run. State terkini ada di `conversations.trip_state`.

### 4.5 Offer

```sql
create table offers (
    id              uuid primary key,
    kind            offer_kind not null,
    supplier        text not null,                       -- 'duffel', 'liteapi', ...
    supplier_ref    text not null,                       -- ID di sisi supplier
    search_key      text not null,                       -- hash parameter pencarian
    price_amount    bigint not null check (price_amount >= 0),
    price_currency  char(3) not null,
    price_idr       bigint not null check (price_idr >= 0),
    fx_rate         numeric(18,8) not null,
    fx_date         date not null,
    summary         jsonb not null,                      -- bentuk ternormalisasi milik kita
    raw             jsonb,                               -- payload supplier, dibuang sesuai retensi
    deeplink_url    text,
    fetched_at      timestamptz not null,
    expires_at      timestamptz not null,
    created_at      timestamptz not null default now(),
    unique (supplier, supplier_ref, fetched_at)
);
create index offers_search_idx on offers (kind, search_key, fetched_at desc);
create index offers_expires_idx on offers (expires_at);
```

Bentuk `summary` per jenis didefinisikan di `MODULE_SPEC.md` (domain `FlightOffer`, `StayOffer`, `ActivityOffer`).

### 4.5a Norma biaya

Angka acuan untuk item `estimate`. Dipisah dari persona pack karena unit persona berupa teks bebas (lihat `AI_SYSTEM_DESIGN.md` §4.1).

```sql
create table cost_norms (
    id                uuid primary key,
    destination_code  text not null,
    category          text not null check (category in
                        ('meal', 'local_transport_daily', 'transport_link', 'attraction', 'visa', 'other')),
    tier              text check (tier in ('hemat', 'sedang', 'nyaman')),
    label             text not null,
    unit              text not null check (unit in
                        ('per_meal', 'per_day', 'per_ride', 'per_person', 'per_trip')),
    amount            bigint not null check (amount >= 0),
    currency          char(3) not null,
    child_factor      numeric(4,2) check (child_factor between 0 and 1),
    details           jsonb not null default '{}',        -- mis. dari/ke, menit, tag atraksi
    source_label      text not null,
    source_url        text,
    verified_at       date not null,
    valid_from        date,
    valid_to          date,
    created_at        timestamptz not null default now(),
    updated_at        timestamptz not null default now()
);
create index cost_norms_lookup_idx on cost_norms (destination_code, category, tier);
```

Baris tidak diubah di tempat bila sudah dirujuk paket: angka baru masuk sebagai baris baru dan baris lama diberi `valid_to`.

### 4.6 Paket

```sql
create table packages (
    id                uuid primary key,
    conversation_id   uuid not null references conversations(id) on delete cascade,
    run_id            uuid not null references runs(id) on delete cascade,
    trip_request_id   uuid not null references trip_requests(id) on delete cascade,
    tier              package_tier not null,
    title             text not null,
    status            package_status not null default 'proposed',
    feasibility       feasibility not null,
    budget_idr        bigint,
    total_idr         bigint not null check (total_idr >= 0),
    live_idr          bigint not null,                   -- jumlah item live
    estimate_idr      bigint not null,                   -- jumlah item estimasi
    buffer_idr        bigint not null default 0,         -- penyangga tak terduga + kurs
    shortfall_idr     bigint not null default 0,         -- kekurangan bila over_budget
    assumptions       jsonb not null default '[]',
    alternatives      jsonb not null default '[]',       -- saran bila tidak muat
    builder_version   text not null,
    created_at        timestamptz not null default now(),
    check (total_idr = live_idr + estimate_idr)
);
create index packages_conversation_idx on packages (conversation_id, created_at desc);

alter table conversations
    add constraint conversations_selected_package_fk
    foreign key (selected_package_id) references packages(id) on delete set null;

create table package_days (
    id           uuid primary key,
    package_id   uuid not null references packages(id) on delete cascade,
    day_index    integer not null check (day_index >= 0),
    date         date,
    title        text not null,
    summary      text,
    unique (package_id, day_index)
);

create table package_items (
    id             uuid primary key,
    package_id     uuid not null references packages(id) on delete cascade,
    day_index      integer,                              -- null untuk item sepanjang perjalanan
    sort_order     integer not null,
    category       item_category not null,
    title          text not null,
    quantity       integer not null default 1 check (quantity > 0),
    unit_amount    bigint not null check (unit_amount >= 0),
    currency       char(3) not null,
    amount_idr     bigint not null check (amount_idr >= 0),
    fx_rate        numeric(18,8) not null,
    price_kind     price_kind not null,
    offer_id       uuid references offers(id) on delete restrict,
    norm_id        uuid references cost_norms(id) on delete restrict,
    source_label   text not null,
    source_url     text,
    priced_at      timestamptz not null,                 -- waktu ambil / tanggal verifikasi
    expires_at     timestamptz,
    details        jsonb not null default '{}',
    check (price_kind <> 'live' or (offer_id is not null and expires_at is not null)),
    check (price_kind <> 'estimate' or norm_id is not null or source_url is not null)
);
create index package_items_package_idx on package_items (package_id, day_index, sort_order);
```

Dua `check` terakhir menegakkan tujuan G2 di tingkat database: item `live` wajib menunjuk offer; item `estimate` wajib punya rujukan.

### 4.7 Indeks biaya dan kurs

```sql
create table price_cache (
    id                 uuid primary key,
    kind               text not null check (kind in ('flight_route_month', 'stay_city_month')),
    origin_code        text,                             -- untuk penerbangan
    destination_code   text not null,
    month              date not null,                    -- tanggal 1 bulan tersebut
    tier               text,                             -- untuk penginapan
    min_idr            bigint not null,
    median_idr         bigint not null,
    sample_count       integer not null,
    source             text not null,
    refreshed_at       timestamptz not null,
    expires_at         timestamptz not null,
    unique (kind, origin_code, destination_code, month, tier)
);
create index price_cache_dest_idx on price_cache (destination_code, month);

create table fx_rates (
    base        char(3) not null,
    quote       char(3) not null,
    rate_date   date not null,
    rate        numeric(18,8) not null check (rate > 0),
    source      text not null,
    created_at  timestamptz not null default now(),
    primary key (base, quote, rate_date)
);
```

### 4.8 Interaksi pengguna

```sql
create table outbound_clicks (
    id               uuid primary key,
    user_id          uuid references users(id) on delete set null,
    package_item_id  uuid references package_items(id) on delete set null,
    offer_id         uuid references offers(id) on delete set null,
    supplier         text not null,
    created_at       timestamptz not null default now()
);
create index outbound_clicks_created_idx on outbound_clicks (created_at);

create table reminders (
    id               uuid primary key,
    user_id          uuid not null references users(id) on delete cascade,
    conversation_id  uuid references conversations(id) on delete cascade,
    kind             text not null check (kind in ('booking_window_open', 'price_recheck')),
    payload          jsonb not null,
    trigger_at       timestamptz not null,
    status           text not null default 'pending' check (status in ('pending', 'sent', 'cancelled', 'failed')),
    created_at       timestamptz not null default now()
);
create index reminders_due_idx on reminders (trigger_at) where status = 'pending';

create table feedback (
    id           uuid primary key,
    user_id      uuid references users(id) on delete set null,
    message_id   uuid references messages(id) on delete cascade,
    package_id   uuid references packages(id) on delete cascade,
    rating       smallint not null check (rating in (-1, 1)),
    reason       text,
    comment      text,
    created_at   timestamptz not null default now(),
    check (message_id is not null or package_id is not null)
);

create table idempotency_keys (
    key            text not null,
    user_id        uuid not null references users(id) on delete cascade,
    request_hash   bytea not null,
    response       jsonb,
    created_at     timestamptz not null default now(),
    primary key (user_id, key)
);
```

## 5. Ringkasan indeks dan alasannya

| Indeks | Melayani |
|---|---|
| `conversations_user_recent_idx` | Daftar percakapan pengguna, terbaru dulu |
| `messages (conversation_id, seq)` unik | Memuat riwayat berurutan; mencegah urutan ganda |
| `runs_one_active_per_conversation` | Satu run aktif per percakapan |
| `runs_running_idx` | Job penyapu run menggantung |
| `run_events_run_seq_idx` | Putar ulang SSE dan pembacaan AI Lab |
| `offers_search_idx` | Mencari offer segar untuk parameter pencarian yang sama |
| `offers_expires_idx` | Job retensi |
| `price_cache_dest_idx` | Query budget → tujuan |
| `reminders_due_idx` | Job pengirim pengingat |

## 6. Migrasi

- Alat: `goose`, berkas SQL di `/migrations`, penamaan `YYYYMMDDHHMMSS_deskripsi.sql`.
- Setiap migrasi punya bagian `Up` dan `Down`.
- Perubahan yang merusak dilakukan dua langkah (expand → migrate → contract) agar bisa dirilis tanpa downtime.
- Migrasi dijalankan sebagai langkah terpisah sebelum deploy binary baru.
- Tabel River dimigrasikan dengan perkakas River sendiri.
- `sqlc` dijalankan terhadap skema hasil migrasi; CI gagal bila kode hasil generate tidak sinkron.

## 7. Siklus hidup data

| Data | Retensi usulan | Mekanisme |
|---|---|---|
| Percakapan tamu | 30 hari sejak aktivitas terakhir | Job harian menghapus `users` tamu (cascade) |
| Percakapan pengguna terdaftar | Sampai dihapus pengguna | — |
| `offers.raw` | 30 hari | Job mengosongkan kolom |
| `offers` | Selama dirujuk `package_items`; lainnya 30 hari | Job harian |
| `tool_artifacts` | 90 hari | Job harian |
| `run_events` | 180 hari di database, lalu diarsipkan | Lepas partisi bulanan |
| `price_cache` | Ditimpa saat refresh | Worker terjadwal |
| `fx_rates` | Permanen | — |
| `idempotency_keys` | 24 jam | Job harian |
| `auth_sessions`, `magic_link_tokens` | Dihapus setelah kedaluwarsa | Job harian |

**Penghapusan akun:** `users.deleted_at` diisi → sesi dicabut → setelah masa tenggang (usul: 7 hari) baris dihapus dengan cascade, dan `UserMemory.Forget(userID)` dipanggil untuk menghapus memori pengguna di OpenPersona (menunggu penghapusan scope di library itu). `outbound_clicks` dan `feedback` dipertahankan tanpa `user_id`.

**Paket dan offer kedaluwarsa:** paket tidak dihapus saat offer kedaluwarsa; statusnya menjadi `stale` dan UI menawarkan cek ulang harga.

## 8. Yang belum ada di skema (sengaja)

| Hal | Kapan |
|---|---|
| Tabel order/pembayaran | Bila booking dalam aplikasi diputuskan |
| Konten KG | Dikelola library KG |
| Tabel konfigurasi prompt | Prompt versi ada di repo; `runs.config_snapshot` mencatat versi yang dipakai |
