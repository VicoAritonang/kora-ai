package pipeline

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/agent/prompts"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/reference"
	"github.com/koraai/koraai/internal/session"
)

var intentCriteria = map[string]string{
	"plan_trip":             "The user wants a trip to a specific place planned or priced, for example they give a destination (optionally with budget, dates or questions about what to do there).",
	"budget_to_destination": "The user has a budget but no destination and asks where they could go with it.",
	"refine":                "The user asks to change an existing plan already in trip_state (fewer nights, other hotel area, other dates, another tier).",
	"info":                  "The user only asks for travel information (visa, weather, festivals, tips) and gives no budget and does not ask for a plan or price.",
	"chitchat":              "Greeting, thanks or small talk with no travel request.",
	"out_of_scope":          "The message is not about travel at all.",
}

// route classifies the message with JEV in one request (§3.1); below the
// threshold or on error it falls back to gpt-5-nano with the same schema.
func (r *Runner) route(ctx context.Context, st *runState) error {
	d := &r.d
	stateJSON := session.CompactState(st.state)
	var last string
	if msgs, err := d.DB.Q().LastMessages(ctx, st.in.ConversationID, 3); err == nil {
		for _, m := range msgs {
			if m.Role == "assistant" {
				last = trimRunes(m.Content, 600)
			}
		}
	}
	dec := RouteDecision{Language: "id"}
	jevOK := false
	if d.Decider != nil {
		res, err := d.Decider.Decide(ctx, ports.DecideRequest{
			Role:  "router",
			State: map[string]any{"message": st.text, "trip_state": stateJSON, "previous_assistant_message": last},
			Questions: map[string]ports.Question{
				"intent":           {Type: "choice", Criteria: intentCriteria},
				"has_budget":       {Type: "noul", Instructions: "Does the message or trip_state state a budget amount for the trip?"},
				"has_destination":  {Type: "noul", Instructions: "Does the message or trip_state name a destination city or country?"},
				"has_dates":        {Type: "noul", Instructions: "Does the message or trip_state mention travel dates, a month or a season?"},
				"changes_slots":    {Type: "noul", Instructions: "Does the message change a value already present in trip_state (destination, budget, dates, nights, party size)?"},
				"needs_fresh_info": {Type: "noul", Instructions: "Does answering need up-to-date information such as this year's event dates, closures or new rules?"},
				"language":         {Type: "choice", Criteria: map[string]string{"id": "The message is in Indonesian", "en": "The message is in English", "other": "Another language"}},
			},
		})
		st.addUsage(res.Usage)
		u := res.Usage
		st.emit(ctx, "model.called", false, "route", map[string]any{"role": "router", "response_id": res.ID, "error": errStr(err)}, &agent.EventMeta{Model: res.Model, Usage: &u, Latency: res.Latency})
		if err == nil {
			a := res.Answers
			dec = RouteDecision{
				Intent: domain.Intent(a["intent"].Choice), Confidence: a["intent"].Confidence,
				HasBudget: a["has_budget"].Yes >= 0.5, HasDestination: a["has_destination"].Yes >= 0.5, HasDates: a["has_dates"].Yes >= 0.5,
				ChangesSlots: a["changes_slots"].Yes >= 0.5, NeedsFreshInfo: a["needs_fresh_info"].Yes >= 0.5, Language: a["language"].Choice,
			}
			jevOK = domain.ValidIntent(string(dec.Intent)) && dec.Confidence >= d.Cfg.JEV.Threshold
			st.emit(ctx, "route.decided", false, "route", map[string]any{"source": "jev", "answers": res.Answers, "threshold": d.Cfg.JEV.Threshold, "accepted": jevOK}, nil)
		}
	}
	if !jevOK {
		fb, err := r.routeFallback(ctx, st, stateJSON, last)
		if err != nil {
			if dec.Intent == "" || !domain.ValidIntent(string(dec.Intent)) {
				return fail("model_unavailable", true, err)
			}
			st.flag("router_fallback_failed") // keep the low-confidence JEV answer
		} else {
			dec = fb
		}
	}
	// Deterministic corrections (documented in IMPLEMENTATION_NOTES.md).
	if dec.Intent == domain.IntentInfo && dec.HasBudget && dec.HasDestination {
		dec.Intent, dec.Overridden = domain.IntentPlanTrip, "info_with_budget_and_destination"
	}
	if dec.Intent == domain.IntentRefine && st.state.Destination == nil && st.state.Budget == nil {
		dec.Intent, dec.Overridden = domain.IntentPlanTrip, "refine_without_state"
	}
	if dec.Language != "en" {
		dec.Language = "id"
	}
	st.route = dec
	st.emit(ctx, "route.decided", false, "route", dec, nil)
	return nil
}

var routerSchema = json.RawMessage(`{"type":"object","additionalProperties":false,
 "required":["intent","has_budget","has_destination","has_dates","changes_slots","needs_fresh_info","language"],
 "properties":{"intent":{"type":"string","enum":["plan_trip","budget_to_destination","refine","info","chitchat","out_of_scope"]},
  "has_budget":{"type":"boolean"},"has_destination":{"type":"boolean"},"has_dates":{"type":"boolean"},
  "changes_slots":{"type":"boolean"},"needs_fresh_info":{"type":"boolean"},"language":{"type":"string","enum":["id","en","other"]}}}`)

func (r *Runner) routeFallback(ctx context.Context, st *runState, stateJSON, last string) (RouteDecision, error) {
	d := &r.d
	res, err := d.Model.Generate(ctx, ports.GenerateRequest{
		Role: "router_fallback", Model: d.Cfg.Models.Extractor, FallbackModel: d.Cfg.Models.ExtractorFallback, Instructions: prompts.Router(),
		Input:           []ports.InputItem{ports.Message(ports.RoleUser, "<trip_state>"+stateJSON+"</trip_state>\n<previous_assistant_message>"+last+"</previous_assistant_message>\n<message>"+st.text+"</message>")},
		MaxOutputTokens: 600, ReasoningEffort: string(r.policy.For("router_fallback", agent.Signals{}).Effort), Schema: &ports.JSONSchema{Name: "route", Schema: routerSchema},
	})
	st.addUsage(res.Usage)
	u := res.Usage
	st.emit(ctx, "model.called", false, "route", map[string]any{"role": "router_fallback", "prompt_version": prompts.RouterVersion, "error": errStr(err)}, &agent.EventMeta{Model: res.Model, Usage: &u, Latency: res.Latency})
	if err != nil {
		return RouteDecision{}, err
	}
	var out struct {
		Intent                                                            string
		HasBudget, HasDestination, HasDates, ChangesSlots, NeedsFreshInfo bool
		Language                                                          string
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(res.Text), &raw); err != nil {
		return RouteDecision{}, err
	}
	b := func(k string) bool { v, _ := raw[k].(bool); return v }
	s := func(k string) string { v, _ := raw[k].(string); return v }
	out.Intent, out.Language = s("intent"), s("language")
	if !domain.ValidIntent(out.Intent) {
		return RouteDecision{}, errInvalid("router fallback intent " + out.Intent)
	}
	return RouteDecision{Intent: domain.Intent(out.Intent), Confidence: 1, HasBudget: b("has_budget"), HasDestination: b("has_destination"),
		HasDates: b("has_dates"), ChangesSlots: b("changes_slots"), NeedsFreshInfo: b("needs_fresh_info"), Language: out.Language, UsedFallback: true}, nil
}

type errInvalid string

func (e errInvalid) Error() string { return string(e) }

var extractSchema = json.RawMessage(`{"type":"object","additionalProperties":false,
 "required":["origin","destination","budget_amount","budget_currency","budget_is_per_person","date_from","date_to","month","season","year","nights","adults","children","interests","special_requests","clear_destination"],
 "properties":{
  "origin":{"type":["string","null"]},"destination":{"type":["string","null"]},
  "budget_amount":{"type":["number","null"]},"budget_currency":{"type":["string","null"],"enum":["IDR","USD","JPY","SGD","MYR","EUR",null]},
  "budget_is_per_person":{"type":["boolean","null"]},
  "date_from":{"type":["string","null"]},"date_to":{"type":["string","null"]},
  "month":{"type":["integer","null"]},"season":{"type":["string","null"],"enum":["spring","summer","autumn","winter","year_end",null]},
  "year":{"type":["integer","null"]},"nights":{"type":["integer","null"]},
  "adults":{"type":["integer","null"]},"children":{"type":["integer","null"]},
  "interests":{"type":"array","items":{"type":"string"}},
  "special_requests":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["kind","text"],
    "properties":{"kind":{"type":"string","enum":["festival","halal","children","accessibility","area","other"]},"text":{"type":"string"}}}},
  "clear_destination":{"type":"boolean"}}}`)

type extraction struct {
	Origin            *string                 `json:"origin"`
	Destination       *string                 `json:"destination"`
	BudgetAmount      *float64                `json:"budget_amount"`
	BudgetCurrency    *string                 `json:"budget_currency"`
	BudgetIsPerPerson *bool                   `json:"budget_is_per_person"`
	DateFrom          *string                 `json:"date_from"`
	DateTo            *string                 `json:"date_to"`
	Month             *int                    `json:"month"`
	Season            *string                 `json:"season"`
	Year              *int                    `json:"year"`
	Nights            *int                    `json:"nights"`
	Adults            *int                    `json:"adults"`
	Children          *int                    `json:"children"`
	Interests         []string                `json:"interests"`
	Special           []domain.SpecialRequest `json:"special_requests"`
	ClearDestination  bool                    `json:"clear_destination"`
}

// extract turns the message into a TripStatePatch (gpt-5-nano, strict
// schema), then normalizes places, money and dates in code (§5).
func (r *Runner) extract(ctx context.Context, st *runState) error {
	d := &r.d
	today := d.Clock.Now().In(jakarta()).Format("2006-01-02")
	call := func() (extraction, error) {
		res, err := d.Model.Generate(ctx, ports.GenerateRequest{
			Role: "extractor", Model: d.Cfg.Models.Extractor, FallbackModel: d.Cfg.Models.ExtractorFallback,
			Instructions:    prompts.Extractor(today),
			Input:           []ports.InputItem{ports.Message(ports.RoleUser, "<trip_state>"+session.CompactState(st.state)+"</trip_state>\n<latest_message>"+st.text+"</latest_message>")},
			MaxOutputTokens: 1200, ReasoningEffort: string(r.policy.For("extractor", agent.Signals{}).Effort),
			Schema: &ports.JSONSchema{Name: "trip_slots", Schema: extractSchema},
		})
		st.addUsage(res.Usage)
		u := res.Usage
		st.emit(ctx, "model.called", false, "extract", map[string]any{"role": "extractor", "prompt_version": prompts.ExtractorVersion, "error": errStr(err)}, &agent.EventMeta{Model: res.Model, Usage: &u, Latency: res.Latency})
		var x extraction
		if err != nil {
			return x, err
		}
		return x, json.Unmarshal([]byte(res.Text), &x)
	}
	x, err := call()
	if err != nil {
		x, err = call() // retry once (§3 step 3)
	}
	if err != nil {
		st.flag("extraction_failed")
		// Continue with the existing state; clarify will ask if needed.
		return nil
	}
	// The extractor must not fill unmentioned slots; party size is the slot
	// small models invent most ("Saya ..." → 2 adults). Accept it only when
	// the message carries a party cue.
	if (x.Adults != nil || x.Children != nil) && !mentionsParty(st.text) {
		x.Adults, x.Children = nil, nil
		st.flag("extractor_party_ignored")
	}
	// Deterministic backups for slots small models miss.
	if x.Nights == nil {
		if n, ok := parseNights(st.text); ok {
			x.Nights = &n
		}
	}
	if x.Destination == nil {
		if name, ok := reference.FindDestination(st.text); ok {
			x.Destination = &name
		}
	}
	// Month and season follow the words in the text, not the model's guess.
	if correctPeriod(&x, st.text) {
		st.flag("extractor_period_corrected")
	}
	patch, unsupported := toPatch(x, domain.DateOf(d.Clock.Now()))
	st.unsupported = unsupported
	if unsupported != "" {
		return nil
	}
	if !patch.Empty() {
		if err := st.state.Apply(patch); err != nil {
			st.flag("patch_rejected")
		}
	}
	st.emit(ctx, "slots.extracted", false, "extract", map[string]any{"raw": x, "patch": patch}, nil)
	st.emit(ctx, "trip_state.updated", true, "extract", map[string]any{"trip_state": st.state}, nil)
	return nil
}

// toPatch normalizes an extraction. Unknown destinations are checked against
// the unsupported list so the user gets an honest out-of-scope answer.
func toPatch(x extraction, today domain.Date) (domain.TripStatePatch, string) {
	var p domain.TripStatePatch
	if x.Origin != nil && *x.Origin != "" {
		if pl, ok := reference.Origin(*x.Origin); ok {
			p.Origin = &pl
		}
	}
	if x.Destination != nil && *x.Destination != "" {
		if pl, ok := reference.Destination(*x.Destination); ok {
			p.Destination = &pl
		} else if reference.IsUnsupported(*x.Destination) {
			return p, *x.Destination
		}
	}
	p.ClearDestination = x.ClearDestination
	if x.BudgetAmount != nil && *x.BudgetAmount > 0 {
		cur := "IDR"
		if x.BudgetCurrency != nil && domain.SupportedCurrency(*x.BudgetCurrency) {
			cur = strings.ToUpper(*x.BudgetCurrency)
		}
		mult := 1.0
		for i := 0; i < domain.MinorUnits(cur); i++ {
			mult *= 10
		}
		p.Budget = &domain.Money{Amount: int64(*x.BudgetAmount*mult + 0.5), Currency: cur}
	}
	p.BudgetIsPerPerson = x.BudgetIsPerPerson
	var w *domain.DateWindow
	if x.DateFrom != nil {
		if f, err := domain.ParseDate(*x.DateFrom); err == nil && f.After(today) {
			w = &domain.DateWindow{From: f, FlexDays: 0}
			if x.DateTo != nil {
				if t, err := domain.ParseDate(*x.DateTo); err == nil && t.After(f) {
					w.To = t
				}
			}
		}
	}
	if w == nil && x.Month != nil && *x.Month >= 1 && *x.Month <= 12 {
		y := domain.NextMonthYear(*x.Month, today)
		if x.Year != nil && *x.Year >= today.Year {
			y = *x.Year
		}
		w = &domain.DateWindow{Month: *x.Month, Year: y, FlexDays: 3}
	}
	if w == nil && x.Season != nil && *x.Season != "" {
		y := domain.NextSeasonYear(*x.Season, today)
		if x.Year != nil && *x.Year >= today.Year {
			y = *x.Year
		}
		w = &domain.DateWindow{Season: *x.Season, Year: y, FlexDays: 3}
	}
	// A window that already ended (the model wrote this year for a month
	// that has passed) moves to the next year.
	if w != nil && w.From.IsZero() {
		for i := 0; i < 2; i++ {
			if _, end, err := w.Range(); err == nil && end.Before(today) {
				w.Year++
			}
		}
	}
	p.DateWindow = w
	if x.Nights != nil && *x.Nights >= 1 && *x.Nights <= 60 {
		p.Nights = x.Nights
	} else if w != nil && !w.From.IsZero() && !w.To.IsZero() {
		n := w.From.DaysUntil(w.To)
		p.Nights = &n
	}
	if x.Adults != nil && *x.Adults >= 1 && *x.Adults <= 9 {
		p.Adults = x.Adults
	}
	if x.Children != nil && *x.Children >= 0 && *x.Children <= 8 {
		p.Children = x.Children
	}
	for _, i := range x.Interests {
		if i = strings.ToLower(strings.TrimSpace(i)); i != "" && len(i) < 40 {
			p.Interests = append(p.Interests, i)
		}
	}
	for _, s := range x.Special {
		if strings.TrimSpace(s.Text) != "" {
			s.Text = trimRunes(s.Text, 200)
			p.Special = append(p.Special, s)
		}
	}
	return p, ""
}

var partyCues = []string{
	"berdua", "bertiga", "berempat", "berlima", "orang", "dewasa", "anak", "istri", "suami", "keluarga", "sekeluarga",
	"teman", "pasangan", "kami", "rombongan", "bayi", "sendiri", "solo", "pax", "people", "adults", "kids", "children",
	"family", "couple", "wife", "husband", "friends", "alone", "ortu", "orang tua", "mama", "papa", "ibu", "bapak",
}

var (
	nightsRe = regexp.MustCompile(`(?i)(\d{1,2})\s*(malam|mlm|nights?)\b`)
	daysRe   = regexp.MustCompile(`(?i)(\d{1,2})\s*(hari|days?)\b`)
	dnRe     = regexp.MustCompile(`(?i)\b\d{1,2}\s*d\s*(\d{1,2})\s*n\b`)
)

// parseNights reads trip length phrases: "5 malam", "4D3N", "seminggu",
// "dua minggu", "6 hari" (→ 5 nights).
func parseNights(text string) (int, bool) {
	t := strings.ToLower(text)
	if m := dnRe.FindStringSubmatch(t); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, n >= 1 && n <= 60
	}
	if m := nightsRe.FindStringSubmatch(t); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, n >= 1 && n <= 60
	}
	switch {
	case strings.Contains(t, "dua minggu") || strings.Contains(t, "2 minggu"):
		return 14, true
	case strings.Contains(t, "seminggu") || strings.Contains(t, "1 minggu") || strings.Contains(t, "a week"):
		return 7, true
	}
	if m := daysRe.FindStringSubmatch(t); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n - 1, n >= 2 && n <= 61
	}
	return 0, false
}

// monthNames maps exact words (Indonesian, English, common abbreviations) to
// months. Exact words only: prefixes would match "janji", "desa", "marah".
var monthNames = map[string]int{
	"januari": 1, "january": 1, "jan": 1, "februari": 2, "february": 2, "feb": 2, "maret": 3, "march": 3, "mar": 3,
	"april": 4, "apr": 4, "mei": 5, "may": 5, "juni": 6, "june": 6, "jun": 6, "juli": 7, "july": 7, "jul": 7,
	"agustus": 8, "august": 8, "agu": 8, "agt": 8, "aug": 8, "september": 9, "sept": 9, "sep": 9,
	"oktober": 10, "october": 10, "okt": 10, "oct": 10, "november": 11, "nov": 11, "desember": 12, "december": 12, "des": 12, "dec": 12,
}

// seasonPhrases maps phrases to seasons; checked in order (year_end first so
// "liburan akhir tahun" is not read as winter).
var seasonPhrases = []struct{ phrase, season string }{
	{"akhir tahun", domain.SeasonYearEnd}, {"tahun baru", domain.SeasonYearEnd}, {"natal", domain.SeasonYearEnd},
	{"year end", domain.SeasonYearEnd}, {"new year", domain.SeasonYearEnd}, {"christmas", domain.SeasonYearEnd},
	{"musim gugur", domain.SeasonAutumn}, {"momiji", domain.SeasonAutumn}, {"autumn", domain.SeasonAutumn}, {"fall", domain.SeasonAutumn},
	{"musim semi", domain.SeasonSpring}, {"sakura", domain.SeasonSpring}, {"spring", domain.SeasonSpring}, {"hanami", domain.SeasonSpring},
	{"musim panas", domain.SeasonSummer}, {"summer", domain.SeasonSummer},
	{"musim dingin", domain.SeasonWinter}, {"salju", domain.SeasonWinter}, {"winter", domain.SeasonWinter},
}

// namedMonth returns the month named in text (0 when none). Words used in a
// date ("10 sampai 16 Maret 2027") count too; exact dates are handled first
// by the caller.
func namedMonth(text string) int {
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !(r >= 'a' && r <= 'z') }) {
		if m, ok := monthNames[w]; ok {
			return m
		}
	}
	return 0
}

// namedSeason returns the season phrase in text ("" when none).
func namedSeason(text string) string {
	t := " " + strings.ToLower(text) + " "
	for _, p := range seasonPhrases {
		if strings.Contains(t, " "+p.phrase) {
			return p.season
		}
	}
	return ""
}

// correctPeriod makes month/season follow what the text actually names:
// a named month wins; otherwise a named season replaces any month the model
// invented; with neither, the model's guesses are dropped.
func correctPeriod(x *extraction, text string) bool {
	if x.DateFrom != nil {
		return false // exact dates take priority
	}
	m, s := namedMonth(text), namedSeason(text)
	changed := false
	switch {
	case m > 0:
		if x.Month == nil || *x.Month != m {
			x.Month, changed = &m, true
		}
		if s != domain.SeasonYearEnd {
			x.Season = nil
		}
	case s != "":
		if x.Season == nil || *x.Season != s || x.Month != nil {
			changed = true
		}
		x.Season, x.Month = &s, nil
	default:
		if x.Month != nil || x.Season != nil {
			changed = true
		}
		x.Month, x.Season = nil, nil
	}
	return changed
}

func mentionsParty(text string) bool {
	t := " " + strings.ToLower(text) + " "
	for _, c := range partyCues {
		if strings.Contains(t, c) {
			return true
		}
	}
	return false
}

func trimRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
