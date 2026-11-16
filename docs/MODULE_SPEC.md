# MODULE_SPEC.md — Spesifikasi Modul

Dokumen 7 dari 10 · Draft v1 · 14 November 2026

Setiap modul adalah satu paket di `internal/` (lihat `PROJECT_STRUCTURE.md`). Tanda tangan Go di sini adalah kontrak rancangan; detail bisa bergeser saat implementasi selama tanggung jawab dan aturan ketergantungannya tetap.

---

## 1. Peta modul

| Modul | Lapisan | Bergantung pada |
|---|---|---|
| `domain` | Inti | — |
| `ports` | Inti | `domain` |
| `packagebuilder` | Inti | `domain` |
| `verifier` | Aplikasi | `domain`, `ports` |
| `session` | Aplikasi | `domain`, `ports`, `store` |
| `agent` | Aplikasi | `domain`, `ports`, `session` |
| `tools` | Aplikasi | `domain`, `ports`, `packagebuilder` |
| `pipeline` | Aplikasi | semua modul aplikasi + `ports` |
| `conversation` | Aplikasi | `domain`, `store`, `pipeline` |
| `auth` | Aplikasi | `domain`, `store` |
| `packages` | Aplikasi | `domain`, `store`, `ports`, `packagebuilder` |
| `priceindex` | Aplikasi | `domain`, `ports`, `store` |
| `costnorm` | Aplikasi | `domain`, `store` |
| `reminder` | Aplikasi | `domain`, `store` |
| `usage` | Aplikasi | `store` |
| `adapters/*` | Infrastruktur | `ports`, `domain` |
| `store` | Infrastruktur | `domain` |
| `transport/http` | Antarmuka | modul aplikasi |

## 2. `domain`

**Tanggung jawab:** tipe inti dan aturan yang tidak bergantung pada apa pun.

```go
type Money struct {
    Amount   int64  // unit terkecil
    Currency string // ISO 4217
}

type PriceKind string // "live" | "estimate"

type TripState struct {
    Version      int
    Origin       *Place
    Destination  *Place
    Budget       *Money
    DateWindow   *DateWindow   // tanggal pasti, atau musim/bulan + tahun
    Nights       *int
    Pax          Pax
    Interests    []string
    Special      []SpecialRequest
    Assumptions  []Assumption  // {Slot, Value, Reason, Source}
    SelectedPackageID *uuid.UUID
}

type TripRequest struct { /* snapshot TripState yang sudah lengkap untuk satu run */ }

type FlightOffer struct {
    ID uuid.UUID; Supplier, SupplierRef string
    Price Money; PriceIDR int64; FX FXQuote
    Outbound, Inbound []Segment
    Stops int; Duration time.Duration; Baggage BaggageInfo
    FetchedAt, ExpiresAt time.Time; Deeplink string
}
type StayOffer struct {
    ID uuid.UUID; Supplier, SupplierRef string
    Name, Area string; Rating float32; Stars int
    PricePerNight, PriceTotal Money; PriceIDR int64; FX FXQuote
    CheckIn, CheckOut Date; Guests int
    DistanceToStationM *int
    FetchedAt, ExpiresAt time.Time; Deeplink string
}
type ActivityOffer struct { /* serupa */ }

type Package struct {
    ID uuid.UUID; Tier Tier; Title string
    Items []PackageItem; Days []PackageDay
    TotalIDR, LiveIDR, EstimateIDR, BufferIDR, ShortfallIDR int64
    Feasibility Feasibility
    Assumptions []Assumption; Alternatives []Alternative
}
```

**Aturan domain di sini:** aritmetika `Money` (tanpa campur mata uang), konversi dengan `FXQuote`, validasi `Pax`, resolusi `DateWindow` ke rentang tanggal.

**Edge case:** penjumlahan lintas mata uang harus gagal saat kompilasi atau panik di uji, tidak diam-diam; pembulatan konversi selalu ke atas (membulatkan biaya ke atas lebih aman bagi pengguna).

## 3. `ports`

**Tanggung jawab:** seluruh interface ke dunia luar. Tanpa implementasi.

```go
// --- AI ---
type ModelProvider interface {
    Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error)
    Stream(ctx context.Context, req GenerateRequest) (Stream, error)
}
type Decider interface { // gaya JEV
    Decide(ctx context.Context, req DecideRequest) (DecideResult, error)
}

// --- Persona (OpenPersona: pack read-only) ---
type PersonaKnowledge interface {
    Core() string    // identitas persona; masuk prefix stabil
    Version() string // hash pack; dicatat di config_snapshot
    // Retrieve menjalankan beberapa query teks paralel dan menggabungkan hasilnya.
    Retrieve(ctx context.Context, queries []KnowledgeQuery, budgetTokens int) (KnowledgePack, error)
}
type KnowledgeQuery struct { Purpose, Text string; Units int }
type KnowledgeUnit  struct { Ref, Kind, Title, Body string; Score float64 } // Ref = "<versi_pack>:<id_node>"
type KnowledgePack  struct { Units []KnowledgeUnit; Gaps []string }         // Gaps = tujuan query tanpa hasil memadai

// --- Memori user (OpenPersona: memory) ---
type UserMemory interface {
    Remember(ctx context.Context, userID uuid.UUID, msgs []MemoryMessage) error
    Recall(ctx context.Context, userID uuid.UUID, query string, maxTokens int) (MemoryBlock, error)
    List(ctx context.Context, userID uuid.UUID) ([]MemoryFact, error)
    Forget(ctx context.Context, userID uuid.UUID) error // butuh penghapusan scope di OpenPersona
}

// --- Guardrails (guard-rails-engine) ---
type Guard interface {
    // Inbound mengembalikan pesan yang sudah ditulis ulang dan handle pertukaran (vault).
    Inbound(ctx context.Context, in GuardInbound) (GuardInboundResult, error)
    // RunTool menilai panggilan, menjalankan exec bila lolos, memeriksa hasil, memperbarui sesi.
    RunTool(ctx context.Context, run GuardToolRun, exec ToolExec) (GuardToolOutcome, error)
    // Outbound memeriksa teks keluar dan memulihkan placeholder.
    Outbound(ctx context.Context, out GuardOutbound) (GuardOutboundResult, error)
}
// ErrGuardBlocked dan ErrToolRefused memetakan guardrails.ErrBlocked dan ErrToolRefused.
// GuardSession (tak tembus pandang bagi modul lain) dibawa di setiap panggilan dan dikembalikan.

// --- AI Lab (milik pemilik produk) ---
type Tracer interface {
    Emit(ctx context.Context, ev RunEvent) error
}

// --- Supplier ---
type FlightSupplier interface {
    Search(ctx context.Context, q FlightQuery) ([]domain.FlightOffer, error)
    Refresh(ctx context.Context, offer domain.FlightOffer) (domain.FlightOffer, error)
}
type StaySupplier interface {
    Search(ctx context.Context, q StayQuery) ([]domain.StayOffer, error)
    Refresh(ctx context.Context, offer domain.StayOffer) (domain.StayOffer, error)
}
type ActivitySupplier interface {
    Search(ctx context.Context, q ActivityQuery) ([]domain.ActivityOffer, error)
}
type FXProvider interface {
    Rate(ctx context.Context, base, quote string, on Date) (domain.FXQuote, error)
}
```

**Error bertipe** yang dikenali semua pemanggil: `ErrUnavailable`, `ErrTimeout`, `ErrRateLimited`, `ErrNotFound`, `ErrOutOfBookingWindow`, `ErrInvalidQuery`.

**Catatan untuk port pemilik produk:** `PersonaKnowledge`, `UserMemory`, dan `Guard` dibentuk mengikuti API OpenPersona dan guard-rails-engine sebagaimana dibaca pada 14 November 2026 (keduanya masih pra-1.0 dan bisa berubah). Tipe kedua library tidak keluar dari adapternya. `Tracer`/`RunEvent` masih usulan karena AI Lab belum dilihat.

## 4. `pipeline`

**Tanggung jawab:** menjalankan satu run dari pesan sampai jawaban; satu-satunya tempat yang tahu urutan langkah.

```go
type Runner interface {
    Start(ctx context.Context, in RunInput) error
    Cancel(ctx context.Context, runID uuid.UUID) error
}
type RunInput struct { RunID, ConversationID, UserID, MessageID uuid.UUID }
```

**Logika:** lihat `AI_SYSTEM_DESIGN.md` §3. Setiap langkah adalah fungsi `func(ctx, *RunState) error`. `RunState` memuat TripState, ContextPack, kandidat, paket, dan penghitung batas.

**Masukan:** ID run. **Keluaran:** pesan asisten, paket, `run_events`, status run.

**Edge case:**

| Kasus | Perilaku |
|---|---|
| Pesan kosong makna ("halo") | Jalur `chitchat`; tanpa retrieval dan tanpa tool |
| Pengguna mengubah slot di tengah percakapan | TripState diperbarui; paket lama `stale`; susun ulang |
| Pembatalan saat loop berjalan | `context` dibatalkan; run `cancelled`; pesan parsial tidak disimpan |
| Kesalahan tak terduga (panic) | Dipulihkan di batas run; run `failed`, `error_code = internal_error` |
| Guardrail memblokir keluaran | Pesan templat aman; run `blocked` |
| Tidak ada kandidat penerbangan sama sekali | Lihat `packagebuilder` §"tanpa kandidat" |

## 5. `router` dan `extractor` (di dalam `pipeline`)

**Router** — masukan: pesan + TripState ringkas. Keluaran:

```go
type RouteDecision struct {
    Intent Intent; Confidence float64
    HasBudget, HasDestination, HasDates, ChangesSlots, NeedsFreshInfo bool
    Language string
    UsedFallback bool
}
```

**Extractor** — masukan: pesan + TripState. Keluaran: `TripStatePatch` (hanya slot yang disebut pengguna).

**Edge case ekstraksi:**

| Masukan | Hasil yang benar |
|---|---|
| "20 juta" | `Budget{20_000_000, IDR}` |
| "20jt berdua" | Budget total, `Pax.Adults = 2`; asumsi "budget untuk berdua" dicatat |
| "sekitar 1500 dolar" | `Budget{150000, USD}` → dikonversi ke IDR dengan kurs hari ini |
| "musim gugur nanti" | `DateWindow{Season: autumn, Year: <musim gugur berikutnya dari hari ini>}` |
| "akhir tahun" | Jendela pertengahan Desember–awal Januari |
| "Jepang" tanpa kota | `Destination` tingkat negara; pemilihan kota jadi asumsi dari KG |
| Budget per orang vs total tidak jelas | Diasumsikan total; dicatat sebagai asumsi |
| Kota asal tidak dikenal | Slot kosong; default dipakai dengan asumsi |

Normalisasi tempat (nama → kode) dilakukan kode dengan tabel referensi, bukan oleh model.

## 6. `session`

**Tanggung jawab:** TripState dan penyusunan konteks.

```go
type StateStore interface {
    Load(ctx context.Context, convID uuid.UUID) (domain.TripState, error)
    Apply(ctx context.Context, convID uuid.UUID, patch TripStatePatch, expectedVersion int) (domain.TripState, error)
}
type ContextBuilder interface {
    Build(ctx context.Context, in ContextInput) (ModelContext, ContextStats, error)
}
```

**Logika:** menyusun enam lapis sesuai anggaran (`AI_SYSTEM_DESIGN.md` §7); menghitung token dengan tokenizer model; memadatkan bila lewat.

**Edge case:** konflik versi TripState (dua tab) → `ErrStateConflict`, klien memuat ulang; percakapan sangat panjang tanpa ringkasan → ringkasan sinkron darurat dengan nano; ContextPack lebih besar dari anggaran → dipotong sesuai prioritas dan dicatat di `context.built`.

## 7. `agent`

**Tanggung jawab:** loop model + tool dalam batas.

```go
type Loop interface {
    Run(ctx context.Context, in LoopInput) (LoopResult, error)
}
type LoopResult struct {
    Flights []uuid.UUID; Stays []uuid.UUID; Activities []uuid.UUID
    DayPlanID *uuid.UUID
    StopReason StopReason // done | step_limit | cost_limit | time_limit
}
type Tool interface {
    Name() string
    Schema() ToolSchema
    Execute(ctx context.Context, args json.RawMessage, rc *RunContext) (ToolResult, error)
}
type ToolResult struct { Digest string; ArtifactID uuid.UUID; OK bool; Code string }
```

**Edge case:**

| Kasus | Perilaku |
|---|---|
| Model memanggil tool yang tidak ada / argumen tidak sah | Error terstruktur dikembalikan ke model; dihitung sebagai langkah |
| Model memanggil tool yang sama dengan argumen sama | Dilayani cache; setelah tiga kali, disuntik peringatan |
| Kuota tool habis | Tool mengembalikan `quota_exhausted`; model diarahkan menyusun paket |
| Model berhenti tanpa kandidat cukup | Satu kali dorongan dengan daftar yang kurang |
| Respons model terpotong | Ulang langkah sekali |

## 8. `tools`

**Tanggung jawab:** implementasi `Tool` (daftar di `AI_SYSTEM_DESIGN.md` §6). Setiap tool: validasi argumen → panggil port → simpan artefak dan offer → bangun digest.

**Edge case per tool:**

| Tool | Kasus | Perilaku |
|---|---|---|
| `search_flights` | Tanggal di luar jendela penjualan | `ErrOutOfBookingWindow` → kembalikan estimasi dari `price_cache`/periode setara, berlabel `estimate`, plus tanggal perkiraan harga tersedia |
| `search_flights` | Nol hasil | Sarankan bandara alternatif atau tanggal ±3 hari (dari tabel referensi) |
| `search_stays` | Hasil ratusan | Dipangkas ke N teratas per tingkat harga sebelum digest |
| `search_stays` | Harga per malam vs total tidak konsisten | Total dari supplier yang dipakai; per malam dihitung ulang |
| `kg_query` | Tidak ada jawaban | Kembalikan `gaps`; model boleh `web_research` |
| `web_research` | Sumber saling bertentangan | Kembalikan keduanya dengan `confidence` rendah |
| `build_packages` | ID offer tidak dikenal/kedaluwarsa | Ditolak dengan daftar ID bermasalah |

## 9. `packagebuilder`

**Tanggung jawab:** menyusun paket dari kandidat. **Fungsi murni:** tanpa jaringan, tanpa waktu sistem (jam disuntikkan), tanpa LLM.

```go
type Input struct {
    Request    domain.TripRequest
    Flights    []domain.FlightOffer
    Stays      []domain.StayOffer
    Activities []domain.ActivityOffer
    DayPlan    DayPlan
    Norms      CostNorms          // dari modul costnorm (tabel cost_norms)
    FX         map[string]domain.FXQuote
    Policy     Policy             // penyangga, definisi tingkat
    Now        time.Time
}
type Output struct {
    Packages  []domain.Package
    Rejected  []Rejection         // kandidat yang ditolak + alasan
}
func Build(in Input) (Output, error)
```

**Algoritma:**

1. **Saring** kandidat: buang yang kedaluwarsa, pax tidak cocok, atau melanggar batasan keras pengguna.
2. **Biaya tetap** per kombinasi: penerbangan + visa + transport bandara PP.
3. **Biaya harian:** penginapan (total supplier) + makan + transport lokal + aktivitas.
   - Makan: hari penuh 3 kali; hari tiba dan pulang dihitung dari jam tiba/berangkat.
   - Transport lokal: norma harian per kota, ditambah ruas antar-area dari rencana harian.
4. **Penyangga:** `buffer = budget × contingency_pct + Σ(item mata uang asing berlabel estimate) × fx_pct`. Default 10% dan 3%.
5. **Profil tingkat** (kebijakan, bisa dikonfigurasi):

| Tingkat | Penerbangan | Penginapan | Makan | Aktivitas |
|---|---|---|---|---|
| Hemat | Termurah dalam batas transit yang wajar | Tingkat bawah dengan rating minimum | Norma hemat | Gratis/murah diutamakan |
| Seimbang | Nilai terbaik (harga × durasi) | Tingkat menengah, dekat stasiun | Norma sedang | Campuran |
| Nyaman | Durasi terpendek / langsung | Tingkat atas, lokasi terbaik | Norma nyaman | Termasuk yang berbayar |

6. **Pilih** kombinasi terbaik per tingkat dengan total + penyangga ≤ budget.
7. **Kelayakan:** `fits` (total + penyangga ≤ budget), `tight` (total ≤ budget tetapi penyangga tidak muat), `over_budget`.
8. **Hilangkan duplikat:** dua tingkat yang menghasilkan kombinasi sama digabung.

**Tanpa budget** (mode tujuan → budget murni): tiga tingkat disusun tanpa batas atas; `feasibility` tidak dihitung.

**Tidak ada yang muat:** kembalikan satu paket termurah berlabel `over_budget` dengan `shortfall_idr`, plus `Alternatives` terurut:

| Urutan | Alternatif | Cara hitung |
|---|---|---|
| 1 | Geser tanggal | Tanggal termurah dalam jendela dari kandidat/`price_cache` |
| 2 | Kurangi malam | Jumlah malam terbesar yang muat |
| 3 | Ganti area menginap | Area lebih murah dari KG |
| 4 | Ganti destinasi | Dari Indeks Biaya Destinasi (Fase 2) |
| 5 | Tambah dana | `shortfall_idr` |

**Tanpa kandidat penerbangan atau penginapan:** item diisi estimasi dari `price_cache` atau norma KG, berlabel `estimate`, dan paket mencatat asumsi "harga live tidak tersedia".

**Edge case:**

| Kasus | Perilaku |
|---|---|
| Penerbangan malam tiba keesokan hari | Check-in memakai tanggal tiba |
| Penerbangan pulang dini hari | Malam terakhir tetap dihitung; makan hari pulang nol |
| Anak-anak | Norma makan dan tiket memakai faktor anak dari KG; bila tidak ada, sama dengan dewasa dan dicatat |
| Kurs tidak tersedia untuk mata uang offer | Kandidat ditolak dengan alasan |
| Budget sangat kecil (di bawah harga penerbangan termurah) | Langsung `over_budget`; alternatif 4 dan 5 saja |
| Budget sangat besar | Tingkat Nyaman tidak dipaksa menghabiskan budget; sisa ditampilkan |
| Offer kedaluwarsa di antara pencarian dan penyusunan | Ditolak; bila semua kedaluwarsa, minta pencarian ulang sekali |

**Pengujian:** uji tabel untuk tiap aturan, uji properti untuk invarian (total selalu sama dengan jumlah item; `fits` selalu berarti total + penyangga ≤ budget), dan uji golden untuk kasus Osaka.

## 10. `verifier`

```go
type Result struct { Violations []Violation; Rubric []RubricAnswer; Passed bool }
func CheckInvariants(p domain.Package, req domain.TripRequest, now time.Time) []Violation
func (v *Verifier) CheckRubric(ctx context.Context, in RubricInput) ([]RubricAnswer, error)
```

Daftar invarian dan rubrik: `AI_SYSTEM_DESIGN.md` §10. Pelanggaran invarian adalah bug; ia dicatat dengan tingkat error dan paket tidak ditampilkan.

## 11. `conversation`

**Tanggung jawab:** percakapan, pesan, dan pembuatan run.

```go
type Service interface {
    Create(ctx, userID) (Conversation, error)
    List(ctx, userID, page) ([]Conversation, string, error)
    Get(ctx, userID, convID) (ConversationDetail, error)
    PostMessage(ctx, userID, convID, content, idemKey) (Message, Run, error)
    PatchTripState(ctx, userID, convID, patch, version) (domain.TripState, error)
    Delete(ctx, userID, convID) error
}
```

**Logika `PostMessage`:** dalam satu transaksi: cek kunci idempotensi → cek tidak ada run aktif → sisipkan pesan (seq berikutnya) → buat run → commit; lalu serahkan ke `pipeline.Runner`.

**Edge case:** kunci idempotensi sama → kembalikan respons tersimpan; kunci sama dengan isi berbeda → `409`; kuota habis → `429` sebelum menulis apa pun; percakapan milik pengguna lain → `404` (bukan `403`, agar tidak membocorkan keberadaan).

## 12. `packages`

**Tanggung jawab:** membaca paket, memilih, cek ulang harga, tautan keluar.

**Cek ulang harga:** untuk setiap item `live`, panggil `Refresh` pada supplier → kumpulkan offer baru → panggil `packagebuilder` dengan kombinasi yang sama → simpan paket baru → paket lama `stale` → kembalikan selisih.

**Edge case:** offer tidak lagi tersedia → item `unavailable`, paket baru tidak dibuat untuk item itu dan pengguna disarankan menyusun ulang; supplier gagal → `502` tanpa mengubah paket; paket milik run yang gagal → tidak bisa dicek ulang.

## 13. `priceindex`

**Tanggung jawab:** mengisi dan membaca Indeks Biaya Destinasi.

```go
type Index interface {
    EstimateTrip(ctx, origin string, month Date, nights int, pax Pax) ([]DestinationEstimate, error)
    FlightEstimate(ctx, origin, dest string, month Date) (Estimate, error)
    StayEstimate(ctx, dest string, month Date, tier string) (Estimate, error)
}
```

**Pengisian (worker):** untuk setiap pasangan (asal didukung × destinasi didukung × 12 bulan ke depan), ambil sampel beberapa tanggal, simpan minimum dan median. Jadwal refresh disesuaikan agar biaya pencarian supplier terkendali.

**Edge case:** data lebih tua dari `expires_at` tetap dipakai tetapi ditandai; bulan di luar jendela penjualan memakai bulan yang sama tahun sebelumnya bila ada, selain itu median tahunan.

## 13a. `costnorm`

**Tanggung jawab:** menyediakan angka acuan biaya yang terstruktur untuk penyusun paket. Modul ini ada karena unit persona pack berupa teks bebas dan tidak boleh menjadi sumber angka paket (lihat `AI_SYSTEM_DESIGN.md` §4.1).

```go
type Norms interface {
    ForTrip(ctx context.Context, destination string, on Date) (CostNorms, error)
    Attractions(ctx context.Context, destination string, tags []string) ([]Norm, error)
}
type Norm struct {
    ID uuid.UUID; Category, Tier, Label, Unit string
    Amount domain.Money; ChildFactor *float64
    SourceLabel, SourceURL string; VerifiedAt Date
}
```

**Isi:** biaya makan per kali per tingkat, transport lokal harian, ruas transport (bandara → pusat kota), tiket masuk atraksi (boleh nol), biaya visa. Diisi kurator lewat berkas impor yang ditinjau, bukan oleh LLM.

**Edge case:** norma tidak ada untuk destinasi/kategori → penyusun paket menolak menyusun item itu dan paket mencatat celahnya (tidak ada nilai default diam-diam); norma melewati ambang usia → tetap dipakai, ditandai sebagai asumsi; norma dengan masa berlaku (`valid_from`/`valid_to`) di luar tanggal perjalanan → diabaikan.

## 14. `auth` dan `usage`

**`auth`:** sesi tamu, magic link, penggabungan tamu → akun, pencabutan sesi. Token disimpan sebagai hash. Edge case: token magic link dipakai dua kali → ditolak; tamu login ke akun yang sudah punya percakapan → digabung, tidak ditimpa.

**`usage`:** penghitung kuota di Redis (jendela harian menurut zona waktu Asia/Jakarta), batas laju per sesi dan per IP, semaphore run global. Edge case: Redis mati → batas konservatif di memori per instance.

## 15. `reminder`

Membuat pengingat (jendela penjualan terbuka, cek ulang harga), job terjadwal mengirim email. Edge case: percakapan dihapus → pengingat ikut terhapus; email gagal → ulang dengan backoff, lalu `failed`.

## 16. Adapter

| Adapter | Port | Catatan implementasi |
|---|---|---|
| `adapters/openai` | `ModelProvider` | Responses API; pemetaan tool call; penghitungan biaya dari pemakaian token; streaming |
| `adapters/jev` | `Decider` | HTTP; versi dipaku; pemetaan tipe jawaban; batas ukuran state |
| `adapters/duffel` | `FlightSupplier` | Offer request; pemetaan ke `FlightOffer`; `expires_at` dari supplier; deteksi di luar jendela penjualan |
| `adapters/liteapi` | `StaySupplier` | Pencarian per kota/area; normalisasi harga total; `margin` dari konfigurasi |
| `adapters/viator` | `ActivitySupplier` | Fase 2 |
| `adapters/fx` | `FXProvider` | Cache harian; cadangan ke kurs hari sebelumnya |
| `adapters/openpersona` (persona) | `PersonaKnowledge` | Di dalam proses: `persona.Open(pack)` saat start, `Persona.Retrieve` per query secara paralel, gabung + buang duplikat + potong sesuai anggaran, tandai celah dari skor. Berkas pack read-only dan dikirim bersama image. |
| `adapters/openpersona` (memori) | `UserMemory` | Klien HTTP ke layanan `openpersona serve` (`/v1/memory/{scope}/messages`, `/search`); scope = ID pengguna. `Forget` dan `List` menunggu fitur di OpenPersona. |
| `adapters/guardrails` | `Guard` | Di dalam proses: satu `guardrails.Engine` dibuat saat start dari berkas kebijakan dan `tools.yaml`; memetakan `Result`/`Decision` ke tipe port; menyerialkan `Session` ke `conversations.guard_session`; `Secret` dari secret manager agar vault dan sesi berlaku lintas instance. |
| `adapters/fakes` | Semua port | Implementasi palsu untuk uji unit dan uji pipeline; bukan untuk dijalankan di lingkungan mana pun |
| `adapters/tracerpg` | `Tracer` | Menulis `run_events` + publish Redis |
| `adapters/tracerlab` | `Tracer` | Meneruskan ke AI Lab; gagal kirim tidak menggagalkan run |

**Aturan semua adapter supplier:** timeout eksplisit; retry satu kali untuk error sementara; circuit breaker; tidak ada tipe supplier yang keluar dari paket; payload mentah disimpan di `offers.raw`; teks bebas dari supplier ditandai tak tepercaya.

## 17. Frontend (`/web`)

| Bagian | Tanggung jawab | Edge case |
|---|---|---|
| Halaman chat | Kirim pesan, tampilkan stream, status fase | Koneksi putus → sambung ulang dengan `Last-Event-ID`; tab kedua → tampilkan run aktif |
| Kartu paket | Tingkat, total, kelayakan, label live/estimasi | Paket `stale` → tombol cek ulang harga |
| Rincian biaya | Daftar item per kategori dan per hari, sumber, waktu ambil | Item kedaluwarsa → penanda |
| Timeline | Rencana per hari | Tanggal belum pasti → "Hari 1, 2, …" |
| Panel asumsi | Daftar asumsi + ubah cepat | Ubah → `PATCH trip-state` lalu kirim pesan susun ulang |
| Tautan keluar | Panggil endpoint outbound lalu buka tab baru | `410` → tawarkan cek ulang harga |
| Akun | Login, profil, memori, hapus akun | — |

Aturan: semua teks antarmuka lewat kamus i18n (Indonesia utama); format rupiah dan tanggal memakai locale `id-ID`; tidak ada perhitungan uang di klien selain pemformatan.
