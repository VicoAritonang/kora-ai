# IMPLEMENTATION_PLAN.md — Rencana Implementasi

Dokumen 9 dari 10 · Draft v1 · 14 November 2026

Durasi di bawah mengandaikan tim minimum: 2 backend Go, 1 frontend, 1 kurator KG (paruh waktu), CTO sebagai arsitek. Durasi adalah perkiraan perencanaan, bukan komitmen.

---

## 1. Ikhtisar fase

| Fase | Durasi | Hasil |
|---|---|---|
| 0 — Keputusan dan spike | 2 minggu | Risiko terbesar terjawab; kontrak port disepakati |
| 1 — Irisan vertikal Osaka | 6 minggu | Query Osaka dari brief menghasilkan tiga paket yang benar, dari web sampai database |
| 2 — Pematangan | 6 minggu | Budget → tujuan, tanggal jauh, memori, penyetelan guardrails, gerbang evaluasi, beta tertutup |
| 3 — Monetisasi dan perluasan | Berkelanjutan | Afiliasi berjalan; destinasi bertambah |

## 2. Ketergantungan antarfitur

```
F0 spike ──► T1 fondasi ──► T2 domain+ports ──┬─► T3 adapters supplier ──┐
                                               ├─► T4 adapters AI ────────┤
                                               ├─► T5 KG stub ────────────┤
                                               └─► T6 packagebuilder ─────┤
                                                                          ▼
                              T7 session ──► T8 agent+tools ──► T9 pipeline ──► T10 API+SSE ──► T11 web
                                                                          │
                                                                          └─► T12 event/tracer ──► T13 eval
```

Jalur kritis: T2 → T6 → T8 → T9 → T10 → T11. T6 (penyusun paket) tidak butuh AI maupun supplier dan bisa dimulai paling awal.

Ketergantungan luar yang bisa menghambat:

| Ketergantungan | Dibutuhkan di | Mitigasi |
|---|---|---|
| OpenPersona dan guard-rails-engine masih berubah (pra-1.0) | T5 | Versi dipaku; tipe library hanya di adapter |
| Penghapusan scope di OpenPersona | P2-6 | Memori tidak diaktifkan sebelum ada |
| Bentuk event AI Lab | T12 | Tulis ke Postgres dulu |
| Dokumen sumber persona dan norma biaya Osaka dari travel agent | T5, T13 | Mulai wawancara di Fase 0 |
| Hasil spike Duffel | T3 | Sumber harga cadangan |

## 3. Fase 0 — Keputusan dan spike (2 minggu)

| ID | Tugas | Keluaran | Kriteria terima |
|---|---|---|---|
| F0-1 | Spike Duffel | Catatan + skrip | Pencarian CGK–KIX dan SUB/DPS–KIX dijalankan untuk ≥ 5 tanggal. Tercatat: maskapai yang muncul (terutama LCC), mata uang, latensi p50/p95, masa berlaku offer, perilaku untuk tanggal > 11 bulan. |
| F0-2 | Spike LiteAPI | Catatan + skrip | Pencarian Osaka untuk ≥ 5 rentang tanggal. Tercatat: jumlah hasil, kelengkapan (area, rating, koordinat), latensi, ketersediaan deep link. |
| F0-3 | Spike JEV | Dataset + laporan | ≥ 100 pesan berlabel (intent + slot yang ada). Tercatat: akurasi per pertanyaan, kurva ambang, latensi, hasil 10 kasus injeksi. |
| F0-4 | Spike model generatif | Laporan | gpt-5-nano untuk ekstraksi pada dataset F0-3: akurasi per slot dan latensi dengan reasoning effort minimal. gpt-5-mini menjalankan loop mainan dengan dua tool. |
| F0-5 | Spike OpenPersona (persona) | Pack percobaan + laporan | Pack Osaka dilatih dari draf dokumen sumber. ≥ 30 pertanyaan uji berbahasa Indonesia dijalankan lewat `Persona.Retrieve`. Tercatat: unit relevan yang terambil/luput (terutama sinonim), pengaruh reranker, ambang skor untuk deteksi celah, biaya training. `replace` di `go.mod` terbukti jalan. |
| F0-6 | Spike guard-rails-engine | `tools.yaml` + laporan | Katalog tool kita lolos validasi. `Inbound`, `ExecuteTool`, `Outbound` dijalankan pada ≥ 50 pesan perjalanan wajar dan ≥ 20 injeksi berbahasa Indonesia. Tercatat: false positive, injeksi yang lolos, pengaruh penyamaran PII pada ekstraksi slot, dan apakah `Outbound` per paragraf memulihkan placeholder dengan benar. |
| F0-7 | Sumber persona + norma biaya Osaka | Dokumen markdown, `spec.json`, berkas norma | Kurator menulis dokumen sumber (karakter area, musim, acara musim gugur beserta tahunnya, heuristik durasi dan budget, prosedur visa) dan `spec.json`. Berkas norma biaya Osaka terisi: makan tiga tingkat, transport bandara, transport harian, ≥ 20 atraksi, visa; setiap baris punya sumber dan tanggal verifikasi. |
| F0-10 | Kebutuhan ke library sendiri | Daftar isu | Tercatat sebagai isu di repo masing-masing: OpenPersona (penghapusan scope, daftar fakta per scope, autentikasi server, module path); kontrak `RunEvent` dengan AI Lab disepakati. |
| F0-8 | Keputusan produk | Catatan keputusan | Lima keputusan di `TECHNICAL_SPEC.md` §2 ditetapkan. |
| F0-9 | Pilih penyedia kurs dan hosting | Catatan keputusan | — |

**Gerbang keluar Fase 0:** bila F0-1 menunjukkan Duffel tidak memuat penerbangan yang wajar untuk rute Indonesia–Jepang, sumber harga kedua ditetapkan sebelum Fase 1 dimulai.

## 4. Fase 1 — Irisan vertikal Osaka (6 minggu)

### Minggu 1 — Fondasi (T1, T2)

| ID | Tugas | Kriteria terima |
|---|---|---|
| T1-1 | Kerangka repo sesuai `PROJECT_STRUCTURE.md`, Makefile, Compose | `make dev` menyalakan Postgres, Redis, api, worker, web |
| T1-2 | CI: lint, uji, generate-check, build image | PR tanpa lint bersih gagal |
| T1-3 | `config`, `platform` (logging, tracing, clock, id, httpclient) | Log JSON dengan `request_id`; trace terlihat di kolektor lokal |
| T1-4 | Migrasi awal: identitas, percakapan, run, event | `migrate-up` dan `migrate-down` bersih |
| T1-5 | `openapi.yaml` v0 + generator Go dan TS | Tipe TS dan server stub tergenerate |
| T2-1 | `domain`: `Money`, `TripState`, offer, paket | Uji: aritmetika uang, konversi, penolakan campur mata uang |
| T2-2 | `ports` + error bertipe | Semua interface di `MODULE_SPEC.md` §3 ada |
| T2-3 | Aturan `depguard` | Impor terlarang menggagalkan lint |

### Minggu 2 — Inti deterministik dan adapter (T3–T6)

| ID | Tugas | Kriteria terima |
|---|---|---|
| T6-1 | `packagebuilder`: saring, biaya tetap, biaya harian, penyangga | Uji tabel per aturan |
| T6-2 | Profil tingkat, kelayakan, alternatif | Kasus budget cukup / pas / kurang lolos; uji properti invarian lolos |
| T6-3 | `verifier.CheckInvariants` | Delapan invarian teruji, masing-masing dengan kasus pelanggaran |
| T3-1 | `adapters/duffel` + uji kontrak dengan rekaman | Pemetaan ke `FlightOffer` benar untuk rekaman F0-1; timeout dan retry teruji |
| T3-2 | `adapters/liteapi` + uji kontrak | Sama, untuk `StayOffer` |
| T3-3 | `adapters/fx` + tabel `fx_rates` | Kurs ter-cache; cadangan ke hari sebelumnya |
| T4-1 | `adapters/openai` (generate, stream, tool call, hitung biaya) | Uji dengan server tiruan; biaya dihitung dari token |
| T4-2 | `adapters/jev` | Uji dengan server tiruan; ambang dari konfigurasi |
| T5-1 | `adapters/openpersona` (persona, di dalam proses) + pack Osaka dari F0-7 | Lima query pipeline menghasilkan `KnowledgePack` untuk Osaka musim gugur; celah terdeteksi untuk pertanyaan di luar pack |
| T5-2 | Modul `costnorm` + tabel `cost_norms` + impor berkas norma | Norma Osaka terbaca; norma hilang menghasilkan error, bukan nilai default |
| T5-3 | `adapters/guardrails` (di dalam proses) + `policy.yaml` + `tools.yaml` + `adapters/tracerpg` | Empat tahap berjalan pada run uji; `Session` tersimpan dan dimuat ulang; `Decision` tertulis ke `run_events` |
| T5-4 | `adapters/fakes` untuk semua port | Uji pipeline berjalan tanpa jaringan |

### Minggu 3 — Sesi dan agen (T7, T8)

| ID | Tugas | Kriteria terima |
|---|---|---|
| T7-1 | `session.StateStore` dengan optimistic locking | Konflik versi terdeteksi |
| T7-2 | `session.ContextBuilder` enam lapis | Anggaran token dipatuhi; statistik per lapis tercatat |
| T8-1 | `agent.Loop` dengan batas langkah/biaya/waktu | Uji dengan model tiruan: berhenti normal, batas langkah, tool gagal |
| T8-2 | Tools: `search_flights`, `search_stays`, `kg_query` | Digest ≤ batas; artefak dan offer tersimpan |
| T8-3 | Tools: `propose_day_plan`, `build_packages` | Menolak ID tak dikenal dan argumen harga |
| T8-4 | Prompt `planner` v1 | Loop menyelesaikan kasus Osaka dengan supplier terekam |

### Minggu 4 — Pipeline (T9)

| ID | Tugas | Kriteria terima |
|---|---|---|
| T9-1 | Router (JEV + cadangan) dan extractor | Akurasi pada dataset F0-3 ≥ target |
| T9-2 | Aturan klarifikasi dan asumsi | Tidak pernah bertanya dua kali; asumsi tercatat |
| T9-3 | Perakitan sepuluh langkah + titik guardrail | Run ujung ke ujung dari CLI menghasilkan paket tersimpan |
| T9-4 | Narator + rubrik verifikasi | Narasi tidak memuat angka di luar paket pada kasus uji |
| T9-5 | Jalur gagal: supplier mati, model mati, batas tercapai | Setiap jalur menghasilkan status dan event yang benar |
| T9-6 | Tool `web_research` (terisolasi, berkuota, ber-cache) | Hasil berlabel tak tepercaya; tidak pernah jadi item `live` |

### Minggu 5 — API dan web (T10, T11)

| ID | Tugas | Kriteria terima |
|---|---|---|
| T10-1 | `auth`: tamu + magic link + CSRF | Tamu bisa chat; login memindahkan percakapan |
| T10-2 | `conversation` + idempotensi + satu run aktif | Kirim ganda menghasilkan satu run |
| T10-3 | SSE dengan putar ulang dan Redis fan-out | Putus-sambung tidak kehilangan event; dua instance api teruji |
| T10-4 | Endpoint paket + tautan keluar | Sesuai `API_SPEC.md` |
| T10-5 | `usage`: kuota dan rate limit | Kuota terlampaui → `429` |
| T11-1 | Halaman chat dengan stream dan status fase | — |
| T11-2 | Kartu paket, rincian biaya, label live/estimasi | Total di layar = total dari API |
| T11-3 | Timeline harian, panel asumsi | Ubah asumsi memicu susun ulang |
| T11-4 | Login, profil | — |

### Minggu 6 — Evaluasi dan pengerasan (T12, T13)

| ID | Tugas | Kriteria terima |
|---|---|---|
| T12-1 | Endpoint internal untuk AI Lab | Seluruh event satu run bisa dibaca |
| T12-2 | Dasbor metrik dasar | Latensi per langkah, biaya per run, hit rate cache |
| T13-1 | Runner golden set + 30 kasus awal | `make eval` menghasilkan laporan |
| T13-2 | Cek ulang harga | Selisih ditampilkan; paket lama `stale` |
| T13-3 | Job: ringkasan, penyapu run, retensi | — |
| T13-4 | Staging + uji end-to-end Playwright | Alur utama lolos di staging |

### Kriteria selesai Fase 1

1. Query dari brief ("orang Jakarta, Rp 20 juta, Osaka, musim gugur, festival") menghasilkan paket di staging, dengan asumsi ditampilkan. Di Fase 1 pengujian memakai tanggal yang masih dalam jendela penjualan supplier; tanggal yang lebih jauh (musim gugur 2027) baru ditangani penuh oleh P2-3, dan sebelum itu sistem menyatakan harga live belum tersedia.
2. Setiap paket lolos delapan invarian; setiap item berlabel `live` atau `estimasi` dengan sumber.
3. Mematikan satu supplier tidak menggagalkan run.
4. Seluruh langkah run terlihat sebagai event.
5. Golden set awal: invarian 100%, kepatuhan batasan ≥ 90%.
6. p95 waktu sampai paket lengkap < 60 detik (target akhir 45 detik di Fase 2).

## 5. Fase 2 — Pematangan (6 minggu)

| ID | Tugas | Bergantung pada | Kriteria terima |
|---|---|---|---|
| P2-1 | `priceindex` + job refresh | T3 | Indeks terisi untuk asal × destinasi × 12 bulan; biaya refresh dalam anggaran |
| P2-2 | Mode budget → tujuan | P2-1 | "Punya 10 juta, bisa ke mana?" dijawab tanpa panggilan supplier live |
| P2-3 | Tanggal di luar jendela penjualan | P2-1 | Query musim gugur 2027 menghasilkan estimasi berlabel + tanggal harga tersedia |
| P2-4 | Pengingat | P2-3 | Email terkirim saat jatuh tempo |
| P2-5 | Peningkatan retrieval persona | F0-5 | Keputusan reranker/embedding berdasarkan data; pack v2 dengan Kyoto dan Tokyo lolos pertanyaan uji |
| P2-6 | Memori user lewat layanan `openpersona serve` | F0-10 (penghapusan scope tersedia) | Preferensi terbawa ke sesi baru; pengguna bisa melihat dan menghapus; layanan mati tidak menggagalkan run |
| P2-7 | Penyetelan kebijakan guardrails | F0-6, T5-3 | Korpus adversarial bahasa Indonesia 100% tertahan; false positive pada pesan wajar di bawah ambang; keputusan tier semantik |
| P2-8 | Penerus event ke AI Lab | F0-6 | Event sampai; kegagalan kirim tidak mengganggu run |
| P2-9 | Aktivitas (Viator) | T8 | Aktivitas berbayar muncul sebagai item `live` |
| P2-10 | Golden set ±50 kasus + gerbang CI | T13 | PR yang menurunkan metrik gagal |
| P2-11 | Perbandingan model di AI Lab | P2-10 | Laporan gpt-5-mini vs kandidat lain per peran |
| P2-12 | Optimasi latensi | — | p95 < 45 detik; tool paralel; prompt cache efektif |
| P2-13 | Ekspor dan hapus akun | — | Penghapusan mencakup KG user |
| P2-14 | Kyoto dan Tokyo di KG | F0-7 | Golden set punya kasus untuk keduanya |
| P2-15 | Uji beban + tinjauan keamanan | — | Lihat `ENGINEERING_STANDARDS.md` §7 |
| P2-16 | Beta tertutup | Semua di atas | — |

### Kriteria selesai Fase 2

Seluruh target di `TECHNICAL_SPEC.md` §4 tercapai di staging, dan beta tertutup berjalan dengan pemantauan.

## 6. Fase 3 — Monetisasi dan perluasan

| Tugas | Catatan |
|---|---|
| Program afiliasi aktif untuk semua tautan keluar | Pelaporan klik → konversi |
| Kemitraan Klook | Aktivitas Asia |
| Destinasi baru | Urutan mengikuti data permintaan beta |
| Kanal WhatsApp | Adapter transport baru |
| Evaluasi booking dalam aplikasi | Keputusan terpisah: perizinan, refund, layanan pelanggan |
| Sumber harga penerbangan kedua | Bila data beta menunjukkan celah LCC |

## 7. Instruksi implementasi

1. **Mulai dari yang deterministik.** `domain` dan `packagebuilder` selesai dan teruji sebelum loop agen ditulis.
2. **Setiap port punya implementasi palsu untuk uji.** OpenPersona dan guard-rails-engine dipasang sungguhan sejak Fase 1; implementasi palsu hanya agar uji berjalan tanpa jaringan dan tanpa berkas pack.
3. **Rekam, jangan panggil.** Uji dan evaluasi memakai rekaman respons supplier dan web; panggilan live hanya di spike dan uji asap terjadwal.
4. **Satu irisan dulu.** Jangan melebarkan destinasi atau fitur sebelum kriteria Fase 1 lolos.
5. **Event sejak baris pertama.** Langkah pipeline baru tidak dianggap selesai tanpa event-nya.
6. **Prompt adalah kode.** Diubah lewat PR, ber-versi, diuji dengan golden set.
7. **Dokumen ikut berubah.** PR yang mengubah kontrak memperbarui dokumen terkait di `/docs`.

## 8. Risiko jadwal

| Risiko | Tanda awal | Tindakan |
|---|---|---|
| Duffel tidak memadai | Hasil F0-1 | Sumber kedua sebelum Fase 1 |
| Konten KG terlambat | F0-7 belum selesai di akhir Fase 0 | Kurangi cakupan ke Osaka saja; tambah waktu kurator |
| Kontrak port berubah besar | F0-5 menemukan banyak ketidakcocokan | Tambah satu minggu di T5/P2-5 |
| Latensi loop terlalu tinggi | T8-4 > 60 detik | Tool paralel, pangkas langkah, pra-pencarian di pipeline |
| Akurasi JEV di bawah target | F0-3 | Router memakai nano sebagai utama, JEV untuk gating/verifikasi saja |
