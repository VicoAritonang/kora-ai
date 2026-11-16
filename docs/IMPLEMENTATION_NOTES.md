# IMPLEMENTATION_NOTES.md — Catatan implementasi

15 November 2026 · Melengkapi sepuluh dokumen rekayasa. Berisi keputusan yang diambil saat menulis kode, penyimpangan dari dokumen beserta alasannya, dan temuan yang perlu ditindaklanjuti. Bila dokumen lain berbeda dengan catatan ini, kode dan catatan ini yang berlaku sampai dokumen terkait diperbarui.

## 1. Status per fase

| Area | Status |
|---|---|
| Fase 1 T1–T13 | Diimplementasikan: fondasi, domain, ports, packagebuilder, verifier, semua adapter, session, agen + tools, pipeline 10 langkah, API + SSE, web, event, eval, job. |
| Fase 2 | Sebagian: Indeks Biaya Destinasi + mode budget → tujuan (P2-1, P2-2), tanggal di luar jendela jual + pengingat (P2-3, P2-4), adapter memori (P2-6, nonaktif), ekspor/hapus akun (P2-13), data Kyoto/Tokyo (P2-14, draf). |
| Belum | Viator (P2-9), dasbor metrik (T12-2), OpenTelemetry exporter (T1-3: log JSON + `run_events` sudah ada), Playwright e2e (T13-4), staging/IaC. |

## 2. Penyimpangan dari dokumen

| Dokumen | Rencana | Implementasi | Alasan |
|---|---|---|---|
| TECH_STACK §2 | `sqlc` + `oapi-codegen` | SQL eksplisit tulis tangan di `store/postgres` (pgx); handler chi tulis tangan. Tipe TypeScript tetap digenerate dari `api/openapi.yaml`. | Go tidak terpasang di mesin pengembang (semua lewat Docker); SQL tetap eksplisit dan teruji integrasi. Migrasi ke sqlc bisa dilakukan per berkas tanpa mengubah pemanggil. |
| TECH_STACK §2 | SDK `openai-go` | Klien HTTP tipis untuk Responses API (`adapters/openai`). | Permukaan yang dipakai kecil (function tools, web search, JSON schema, streaming); format kabel bisa diuji dengan rekaman. Tipe tetap tidak keluar dari adapter (D6). |
| TECH_STACK §2 | `goose` | Migrasi berformat goose di `migrations/`, tetapi sumbernya `db/schema.app.sql` + `db/river.sql`, digenerate oleh `scripts/build-schema.mjs` bersama `db/schema.sql` dan `db/seed.sql`. | Pemilik produk menerapkan skema lewat terminal Neon. Satu sumber, tiga keluaran, dicek sinkron di CI. |
| PROJECT_STRUCTURE §1 | `adapters/*` tidak mengimpor `store` (D5) | `adapters/tracerpg` mengimpor `store`. | Tugas adapter itu memang persistensi `run_events`; dikecualikan di `depguard`. |
| PROJECT_STRUCTURE §1 | — | Modul baru: `internal/account` (profil, ekspor, feedback, memori, pembacaan run untuk AI Lab), `internal/reference` (kode kota/bandara), `adapters/sandbox`, `adapters/email`. | `transport/http` tidak boleh menyentuh `store` (D7); data referensi butuh rumah. |
| AI_SYSTEM_DESIGN §3 | Verifikasi (8) sebelum narasi (9), perbaikan sekali bila rubrik gagal | Invarian kode sebelum narasi. Narasi di-stream per paragraf; tiap paragraf lewat **cek angka deterministik** (`verifier.NumbersNotInPackages`) lalu `Guard.Outbound`. Paragraf dengan nominal yang tidak ada di paket ditahan. Rubrik JEV berjalan sesudahnya: gagal → flag `verification_low_confidence` (AI Lab); bila menyangkut angka → notice ke pengguna + flag `verification_failed`. | Paragraf yang sudah dikirim tidak bisa ditulis ulang tanpa merusak streaming; cek angka kode lebih kuat dari rubrik lunak. |
| AI_SYSTEM_DESIGN §5 | Loop dimulai dari nol | **Pra-pencarian di pipeline** (`pipeline/prefetch.go`): penerbangan + aktivitas paralel, lalu hotel untuk tanggal tiba mayoritas, lewat guard/kuota/event yang sama, diberikan ke planner sebagai panggilan tool miliknya. Loop dihentikan oleh **kode** setelah `build_packages` menghasilkan paket yang muat. | Mitigasi yang sudah tercantum di IMPLEMENTATION_PLAN §8. Hasil terukur: 97 dtk → 26–37 dtk per run, biaya ±$0,009 → ±$0,005. |
| AI_SYSTEM_DESIGN §3.1 | Routing murni JEV | Dua koreksi deterministik: `info` + budget + tujuan → `plan_trip`; `refine` tanpa state → `plan_trip`. | JEV memilih `info` (0,73) untuk query brief yang jelas meminta rencana. |
| MODULE_SPEC §5 | Extractor mengisi slot yang disebut | Penjaga kode: jumlah orang hanya diterima bila pesan memuat penanda rombongan; bulan diabaikan bila hanya musim yang disebut. | gpt-5-nano mengisi `adults: 2` dari "Saya…" dan `month: 10` dari "musim gugur" pada uji nyata. |
| DATABASE_SCHEMA §4 | — | Tambahan (ditandai `[impl]` di `db/schema.app.sql`): enum `feasibility` + `unbounded`; `package_items.price_cache_id` (estimasi dari indeks) dan `fx_date` (invarian 8); `packages.flight_offer_id/stay_offer_id` (cek ulang harga); `package_days.area`; `reminders.attempts`; `price_cache.origin_code/tier` NOT NULL default `''`; fungsi `kora_ensure_run_events_partition`. | Kebutuhan dari invarian, re-pricing dan unique key. |
| DATABASE_SCHEMA §6 | Tabel River lewat perkakas River | SQL River v0.51.2 disertakan di `db/schema.sql` (+ baris `river_migration`). Worker memakai `PollOnly` karena endpoint pooled Neon tidak mempertahankan `LISTEN`. | Skema diterapkan manual di Neon. |
| TripRequest | Kembali = berangkat + malam | Kembali = berangkat + malam + 1; check-in = tanggal tiba setiap offer. | Penerbangan Indonesia → Jepang umumnya tiba esok hari (PLAN §6.6 invarian 3). |

## 3. Temuan pada library pemilik produk

| Library | Temuan | Dampak | Saran |
|---|---|---|---|
| guard-rails-engine | `LoadPolicy` menimpa `Enabled` dari overlay: blok guard tanpa `enabled: true` **diam-diam menonaktifkan guard** (pii, tool_policy, dst.). | Terdeteksi oleh uji: email tidak disamarkan. Diatasi dengan `enabled: true` eksplisit di `guardrails/policy.yaml`. | Gunakan `*bool` di overlay, atau tolak blok tanpa `enabled`. |
| guard-rails-engine | Vanity import `go.vicoworks.com/guard-rails-engine` mengembalikan 404 untuk `?go-get=1`. | Perlu `replace` ke GitHub di `go.mod`. | Pasang meta `go-import`. |
| guard-rails (16 Nov 2026) | Vanity host kini melayani `go.vicoworks.com/guard-rails` → repo guard-rails-engine, tetapi `go.mod` repo (commit `2b8325f`) masih `module go.vicoworks.com/guard-rails-engine`; `go get` gagal: *module declares its path as … but was required as …*. Path lama tetap 404. | Kora tetap memakai `guard-rails-engine` + `replace` sampai repo diperbarui. | Ganti path module di `go.mod` dan seluruh import repo (112 berkas; sudah diuji: build, vet, test lulus), push, beri tag. Lalu di Kora: ganti import di `adapters/guardrails`, hapus `replace`, `go mod tidy`. |
| agent-reliability-lab (16 Nov 2026) | Go SDK adalah module bersarang `go.vicoworks.com/agent-reliability-lab/sdk/go` (tag `sdk/go/vX`), ter-resolve lewat vanity host dan proxy. `Client.Emit` menahan mutex dan menunggu ACK per event. | Dipanggil langsung dari pipeline, setiap event (termasuk tiap paragraf narasi) akan menunggu satu round-trip ke Lab. | Adapter `arllab` mengantrekan event dan mengirim dari satu goroutine. Saran untuk SDK: opsi pengiriman async/batch. |
| OpenPersona | Module path berbeda dari lokasi repo (sudah tercatat). | `replace` di `go.mod`. | — |
| OpenPersona | Retrieval leksikal: "festival musim gugur Osaka" menaruh unit Danjiri di peringkat 5. | Diatasi dengan ekspansi sinonim di query pipeline (`synonyms` di `pipeline/plan.go`) dan 6 unit untuk query permintaan khusus. | Tier embedding/reranker (P2-5). |
| JEV (OpenRouter) | Endpoint: `POST https://openrouter.ai/api/alpha/decisions`; tipe pertanyaan `choice` (criteria map), `noul` (instructions → probabilitas "ya"), `score` (criteria list → **indeks harapan 0..n−1**, bukan 0..1). | Skor dinormalisasi di adapter. | Dokumentasikan skala `score`. |

## 4. Data yang masih draf (wajib ditinjau kurator — F0-7)

- `data/norms/*.json` → `db/seed.sql`: 73 norma biaya Osaka, Kyoto, Tokyo. Semua berlabel `DRAF Kora (belum diverifikasi kurator)`; label ini tampil di UI. Angka adalah perkiraan placeholder, bukan hasil verifikasi.
- `persona/sources/*.md` → `persona/packs/kora.persona` (63 unit): draf yang ditulis sebagai placeholder. Ganti dengan dokumen hasil wawancara travel agent, lalu `scripts/train-persona.sh` dan tinjau dengan `persona/questions/review.txt`.
- Supplier `sandbox`: harga simulasi deterministik. Diberi label "Sandbox (data simulasi, bukan harga nyata)" di setiap item dan banner di UI.

## 5. Hasil terukur (Postgres lokal, sandbox, model asli)

Kasus brief ("orang Jakarta, 20 juta, Osaka, musim gugur, festival"): lolos; 2 paket (Hemat, Seimbang; Nyaman tidak muat dengan penyangga 10%); musim gugur 2027 ter-resolve; unit Danjiri dan visa terambil; biaya ±$0,005; ±37 dtk. Hasil golden set lengkap ada di `eval/reports/` (tidak di-commit).

## 6. Perbaikan dari golden set pertama (26/30 → 4 kasus gagal diperbaiki)

- Tahun yang membuat jendela tanggal berakhir di masa lalu digeser ke tahun berikutnya (nano menulis `year: 2026` untuk "April" pada 15 Nov 2026; planner lalu mencari tanggal lampau 6×).
- Fallback deterministik bila extractor melewatkan slot: durasi ("seminggu", "5 malam", "4D3N") dan destinasi (pindai alias kota).
- Cek angka narasi: bentuk "Rp …" harus cocok sampai ribuan; pembulatan juta hanya untuk bentuk "x juta".
- Indeks Biaya Destinasi: tingkat Hemat memakai harga minimum sampel, tingkat lain median.
- Ekspektasi `injection-id`: diblokir guardrail adalah hasil yang benar.

## 7. Perbaikan dari golden set kedua (27/30)

- **Offer kedaluwarsa di tengah run** (MODULE_SPEC §9): cache offer di Postgres menyajikan offer berumur 29,8 menit (TTL 30 menit) yang kedaluwarsa sebelum `build_packages`. Kini cache hanya menyajikan offer dengan sisa masa berlaku ≥ 10 menit (`tools.cacheMargin`), dan `build_packages` mengembalikan `offers_expired` dengan arahan mencari ulang (sebelumnya `no_combination`, sehingga planner mengulang build 6 kali).
- **Bulan/musim dibaca kode**: gpt-5-nano sesekali salah membaca "Maret" → Mei dan "akhir tahun" → Januari. `correctPeriod` membaca nama bulan dan frasa musim (Indonesia/Inggris, kata utuh) dari teks; bila teks menyebutnya, bacaan kode menang; bila tidak menyebut apa pun, tebakan model dibuang.
- **Latensi planner**: satu langkah `propose_day_plan` bisa memakan ±40 dtk pada reasoning `low`. Uji 4 kasus dengan `MODEL_PLANNER_EFFORT=minimal`: semua lolos, waktu 25–52 dtk (vs 31–91 dtk). Default tetap `low` sesuai AI_SYSTEM_DESIGN §2; `minimal` layak dibandingkan di AI Lab sebelum diadopsi.
- **Panggilan model yang menggantung** (golden set ketiga, 29/30): satu panggilan planner menggantung ±95 dtk dan menghabiskan batas run 120 dtk walau kandidat sudah lengkap. Kini deadline loop juga membatasi panggilan yang sedang berjalan; melewatinya mengakhiri loop (`time_limit`) dan pipeline menyusun paket dari kandidat yang ada (NFR-12). Diuji dengan model palsu yang menggantung.
- Golden set ketiga (setelah semua perbaikan kecuali yang terakhir): 29/30 lolos, biaya rata-rata $0,0045/run, median ±38 dtk, p95 ±102 dtk. Ekor latensi berasal dari langkah reasoning planner; target Fase 1 (p95 < 60 dtk) belum tercapai.

## 8. Effort adaptif (auto) — 15 Nov 2026

Riset praktik terkini (OpenAI, Anthropic, Gemini, studi 2026): rendah untuk ekstraksi/klasifikasi, sedang untuk perencanaan, tinggi hanya bila eval membuktikan; model buruk memilih effort sendiri di awal; kebijakan terbaik mulai rendah lalu **naik sekali setelah kegagalan terverifikasi**. Implementasi (`internal/agent/effort.go`, `internal/pipeline/effort.go`):

| Peran | Auto |
|---|---|
| extractor, router cadangan, chitchat, summarizer | minimal |
| web researcher | low (web search butuh ≥ low) |
| planner | low; medium bila skor sinyal ≥ 2 (permintaan khusus ×2, anak, refine, di luar jendela jual, ≥2 minat) |
| narrator / info | low; medium bila over budget atau ada permintaan khusus |

Eskalasi sekali, dibatasi `MODEL_EFFORT_MAX` (default medium): per langkah setelah tool gagal yang bisa diperbaiki model (argumen salah, kombinasi tidak cocok, offer kedaluwarsa); per run bila tidak ada paket (planner diulang dengan kandidat yang sama); narasi bila semua paragraf ditahan cek angka. Level tetap per peran lewat `MODEL_<ROLE>_EFFORT`. Adapter OpenAI memetakan level ke yang didukung model (`minimal` hanya keluarga GPT-5 asli; diganti `low` untuk 5.1+/web search). Setiap keputusan menjadi event `effort.decided` (publik, tampil di UI; bahan kalibrasi AI Lab).

## 9. Kora sebagai aktuator web — workspace state streaming (NDJSON)

- `GET /api/v1/runs/{id}/events` mengalirkan event run sebagai **NDJSON** (`{"seq","type","data"}` per baris, sambung ulang `?after=N`). SSE tetap ada.
- Pipeline memproyeksikan progres ke **workspace** (`internal/pipeline/workspace.go`): `state.reset` (snapshot awal, termasuk paket yang sudah ada), `state.patch` (RFC 6902), `ui.action` (`focus` panel, `highlight` paket). Kanvas web terisi langsung: kandidat penerbangan → penginapan → rencana harian → pratinjau paket → paket final + rekomendasi + langkah berikutnya. Semua angka dari kode; proporsi bar dihitung server.
- Tool baru `present_to_user` (katalog guardrails + allow-list): planner memilih tampilan dan tingkat yang direkomendasikan; tanpa itu rekomendasi deterministik (Seimbang yang *fits*, lalu Hemat; *tight* hanya bila tidak ada yang fits).
- `POST /api/v1/conversations/{id}/actions`: klik di kanvas (`set_slot`, `shift_dates`, `prefer_area`, `near_station`, `tier_shift`, `ask`) menjadi patch TripState + pesan berlabel aksi + run baru.
- Web: store `zustand` diisi stream NDJSON (`web/src/lib/stream`, `web/src/lib/workspace`), layout dua panel (chat + kanvas), toggle "Ikuti Kora" (fokus agen dihentikan saat pengguna memilih tab sendiri), tombol aksi, mode seluler dengan tab Chat/Rencana.
- Diverifikasi di browser (Playwright headless, desktop + 390 px) terhadap API + Postgres sementara. Temuan saat uji dan sudah diperbaiki: malam bertambah satu bila penerbangan tiba di hari yang sama (prefetch kini memilih kelompok kedatangan yang pas dengan jumlah malam); kanvas meluber di layar sempit.

## 10. Agent Reliability Lab (arl/1) — 16 Nov 2026

Setiap run bisa dialirkan ke Agent Reliability Lab untuk inspeksi saat development, lewat SDK resmi `go.vicoworks.com/agent-reliability-lab/sdk/go` v0.4.1.

- **Adapter** `internal/adapters/arllab` mengimplementasikan `ports.Tracer` dan menjadi sink *best-effort* setelah Postgres (Postgres tetap trace otoritatif). Pengiriman asinkron dengan antrean 4096 event; antrean penuh → event dibuang dari Lab saja.
- **Aktif hanya bila** `STAGE=dev`, `LAB_WS_URL` terisi, **dan** `APP_ENV=local` (config menolak kombinasi ini di luar local). Fail-open: Lab mati tidak menghentikan run; `LAB_REQUIRED=true` untuk tes strict.
- **Fase** yang dideklarasikan: `input, guard, understand, retrieve, plan, build, verify, narrate, output`. Setiap pergantian langkah pipeline membuka `span.start/span.end`; panggilan model dan tool bersarang di span langkahnya.
- **Pemetaan**: `run.started` → `run.start` + `state.snapshot` (config run); `input.received` (event baru, teks **setelah** Guard.Inbound) → `input`; `model.called` → `model.request/response` + `cost.record` (biaya dihitung dari harga token → `estimated`); `tool.called` → `tool.call/result`; `retrieval.done` → `retrieval.result`; `verify.done` dengan `passed`/paragraf ditahan → `evaluation`; paragraf `message.delta` → satu `output`; `run.completed/failed` → `run.end`. `state.patch`, `state.reset`, `tool.started/finished` tidak dikirim (proyeksi UI). Event lain dikirim apa adanya sebagai event kustom.
- **Batasan**: Kora tidak merekam isi prompt; `model.request` menyatakan itu secara eksplisit (role + versi prompt). Replay dari Workbench (`OnReplay`), prompt registry, dan penerusan feedback 👍/👎 ke Lab belum dihubungkan.
- **Tes**: `arllab_test.go` (pemetaan, span, output, kegagalan, payload besar, emit setelah close) dan `protocol_test.go` (SDK asli melawan server arl/1 minimal yang menegakkan aturan envelope `connection.md`). `config_test.go` menguji aturan hanya-local.

## 11. Dua bahasa (EN default, ID) — 16 Nov 2026

- **Web**: kamus `en` lengkap di `web/src/i18n` (tanpa `...id`, jadi TypeScript menolak kunci yang belum diterjemahkan). Pilihan bahasa disimpan per browser (`localStorage` `kora.locale`), default `en`; tombol EN | ID di navbar. Format uang/tanggal mengikuti bahasa (`setFormatLocale`). Konten perjalanan kurasi punya teks EN (`journeysFor`), termasuk prompt yang dikirim ke Kora.
- **Profil**: bahasa UI disinkronkan ke `users.locale` (sekali per sesi dan setiap kali tombol diganti, best effort).
- **Backend**: jawaban Kora, label fase, dan notice mengikuti bahasa *pesan* (router). Pesan yang ditulis server atas nama pengguna untuk aksi kanvas (`conversation/actions.go`) kini mengikuti `users.locale`, sehingga tombol di mode EN tidak memicu jawaban berbahasa Indonesia.
