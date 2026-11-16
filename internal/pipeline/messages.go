package pipeline

import (
	"fmt"
	"strings"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/verifier"
)

// User-facing texts come from a dictionary keyed by code, in the user's
// language, without internal detail (ENGINEERING_STANDARDS §3.4).
var errorMessages = map[string][2]string{
	"blocked_by_guardrail": {"Maaf, permintaan ini tidak bisa saya proses. Coba tulis ulang kebutuhan perjalanan Anda.", "Sorry, I can't process this request. Please rephrase your travel question."},
	"model_unavailable":    {"Layanan AI sedang sibuk. Silakan coba lagi sebentar lagi.", "The AI service is busy. Please try again shortly."},
	"kg_unavailable":       {"Basis pengetahuan travel agent sedang tidak tersedia, jadi saya belum bisa menyusun rencana yang bisa dipertanggungjawabkan. Coba lagi nanti.", "The travel knowledge base is unavailable, so I can't build a reliable plan right now."},
	"budget_exceeded":      {"Permintaan ini melebihi batas biaya pemrosesan. Coba persempit permintaannya.", "This request exceeded the processing cost limit. Please narrow it down."},
	"step_limit_reached":   {"Rencana terlalu rumit untuk diselesaikan sekaligus. Coba persempit permintaannya.", "The plan was too complex to finish at once."},
	"timeout":              {"Waktu pemrosesan habis. Silakan kirim ulang pesan Anda.", "Processing timed out. Please send your message again."},
	"interrupted":          {"Pemrosesan terputus karena pemeliharaan server. Silakan kirim ulang.", "Processing was interrupted. Please send again."},
	"cancelled":            {"Dibatalkan.", "Cancelled."},
	"internal_error":       {"Terjadi kesalahan di sistem kami. Silakan coba lagi.", "Something went wrong on our side. Please try again."},
	"no_packages":          {"Saya belum bisa menyusun paket untuk permintaan ini.", "I couldn't build packages for this request."},
}

func userMessage(code, lang string) string {
	m, ok := errorMessages[code]
	if !ok {
		m = errorMessages["internal_error"]
	}
	if lang == "en" {
		return m[1]
	}
	return m[0]
}

var phaseLabels = map[string][2]string{
	"understanding":        {"Memahami permintaan Anda", "Understanding your request"},
	"retrieving_knowledge": {"Membuka pengetahuan travel agent", "Reading travel-agent knowledge"},
	"searching_flights":    {"Mencari penerbangan", "Searching flights"},
	"searching_stays":      {"Mencari penginapan", "Searching stays"},
	"planning":             {"Menyusun rencana perjalanan", "Planning the trip"},
	"researching_web":      {"Riset informasi terbaru", "Researching recent information"},
	"building_packages":    {"Menyusun paket dan menghitung biaya", "Building packages"},
	"verifying":            {"Memeriksa ulang angka dan tanggal", "Checking numbers and dates"},
	"writing":              {"Menulis penjelasan", "Writing the answer"},
	"estimating":           {"Menghitung perkiraan biaya destinasi", "Estimating destination costs"},
}

func phaseLabel(phase, lang string) string {
	l, ok := phaseLabels[phase]
	if !ok {
		return phase
	}
	if lang == "en" {
		return l[1]
	}
	return l[0]
}

func clarifyQuestion(lang string) (string, []string) {
	if lang == "en" {
		return "Happy to help! Do you already have a destination in mind, or a budget you'd like to stay within?",
			[]string{"I want to go to Osaka", "I have IDR 15 million — where can I go?"}
	}
	return "Siap bantu! Anda sudah punya tujuan, atau punya budget tertentu yang ingin dipakai?",
		[]string{"Saya mau ke Osaka", "Punya 15 juta, bisa ke mana?"}
}

func outOfScopeMessage(lang, unsupported string) string {
	if unsupported != "" {
		if lang == "en" {
			return fmt.Sprintf("Kora doesn't cover %s yet. For now I can plan trips to Japan (Osaka, Kyoto, Tokyo) from Jakarta, Surabaya and Denpasar. Want me to price one of those?", title(unsupported))
		}
		return fmt.Sprintf("Maaf, Kora belum mendukung %s. Saat ini saya bisa merencanakan perjalanan ke Jepang (Osaka, Kyoto, Tokyo) dari Jakarta, Surabaya, dan Denpasar. Mau saya hitungkan salah satunya?", title(unsupported))
	}
	if lang == "en" {
		return "I'm a travel-planning assistant, so I can't help with that. I can estimate a trip budget to Japan, or suggest where a budget can take you."
	}
	return "Maaf, saya khusus membantu merencanakan perjalanan, jadi pertanyaan itu di luar kemampuan saya. Saya bisa menghitung budget perjalanan ke Jepang, atau menyarankan tujuan yang muat dengan budget Anda."
}

func title(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// templateNarration is the deterministic fallback when the narrator model
// fails or its output does not pass checks. Every number comes from the
// packages.
func templateNarration(pkgs []domain.Package, notices []string, sandbox bool) string {
	var b strings.Builder
	if len(pkgs) == 0 {
		return userMessage("no_packages", "id")
	}
	first := pkgs[0]
	if first.Feasibility == domain.FeasibilityOverBudget {
		fmt.Fprintf(&b, "Budget Anda belum cukup untuk perjalanan ini: paket termurah totalnya %s, dengan kekurangan sekitar %s setelah penyangga.\n\n",
			verifier.Rupiah(first.TotalIDR), verifier.Rupiah(first.ShortfallIDR))
	} else {
		fmt.Fprintf(&b, "Berikut %d pilihan paket yang saya susun.\n\n", len(pkgs))
	}
	for _, p := range pkgs {
		fmt.Fprintf(&b, "%s: total %s (live %s, estimasi %s).", p.Title, verifier.Rupiah(p.TotalIDR), verifier.Rupiah(p.LiveIDR), verifier.Rupiah(p.EstimateIDR))
		if p.BudgetIDR != nil && p.Feasibility != domain.FeasibilityOverBudget {
			fmt.Fprintf(&b, " Sisa budget setelah penyangga %s: %s.", verifier.Rupiah(p.BufferIDR), verifier.Rupiah(p.RemainingIDR()))
		}
		b.WriteString("\n\n")
	}
	if len(first.Alternatives) > 0 {
		b.WriteString("Pilihan agar muat:\n")
		for _, a := range first.Alternatives {
			b.WriteString("- " + a.Label + "\n")
		}
		b.WriteString("\n")
	}
	for _, n := range notices {
		b.WriteString(n + "\n\n")
	}
	if sandbox {
		b.WriteString("Catatan: harga penerbangan dan hotel saat ini berasal dari data simulasi (sandbox), bukan penawaran nyata.\n\n")
	}
	b.WriteString("Harga live bisa berubah; makan, transport lokal, dan tiket masuk adalah estimasi. Aturan visa sebaiknya dicek ke situs resmi Kedutaan Jepang. Mau saya ubah durasi, tanggal, atau tingkat hotelnya?")
	return b.String()
}

var seasonID = map[string]string{
	domain.SeasonSpring: "musim semi", domain.SeasonSummer: "musim panas", domain.SeasonAutumn: "musim gugur",
	domain.SeasonWinter: "musim dingin", domain.SeasonYearEnd: "akhir tahun",
}

var monthID = []string{"", "Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli", "Agustus", "September", "Oktober", "November", "Desember"}

// windowLabel renders a date window in Indonesian ("musim gugur 2027").
func windowLabel(w domain.DateWindow) string {
	switch {
	case !w.From.IsZero():
		return fmt.Sprintf("%d %s %d", w.From.Day, monthID[w.From.Month], w.From.Year)
	case w.Month > 0:
		return fmt.Sprintf("%s %d", monthID[w.Month], w.Year)
	case w.Season != "":
		return fmt.Sprintf("%s %d", seasonID[w.Season], w.Year)
	}
	return ""
}
