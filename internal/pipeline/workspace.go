package pipeline

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/priceindex"
)

// The workspace is the live state of the trip canvas in the web app. The
// pipeline projects its progress onto it and streams the changes:
//
//	state.reset  {state}                 full snapshot (first event of a run)
//	state.patch  {ops:[RFC 6902 ops]}    incremental changes
//	ui.action    {action, view, target}  actuator commands (focus, highlight)
//
// Every number shown on the canvas comes from code (offers, norms, the
// package builder), never from model text.

// FlightCard is a flight candidate on the canvas.
type FlightCard struct {
	ID          string   `json:"id"`
	Carriers    []string `json:"carriers"`
	Depart      string   `json:"depart"` // local time + airport
	Arrive      string   `json:"arrive"`
	ReturnDep   string   `json:"return_depart"`
	ReturnArr   string   `json:"return_arrive"`
	ArrivalDate string   `json:"arrival_date"`
	Stops       int      `json:"stops"`
	Hours       float64  `json:"hours"`
	BaggageKg   int      `json:"baggage_kg"`
	PriceIDR    int64    `json:"price_idr"`
	Kind        string   `json:"kind"` // live | estimate
	Sandbox     bool     `json:"sandbox"`
	UsedBy      []string `json:"used_by"` // tiers using it
}

// StayCard is a stay candidate.
type StayCard struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"` // untrusted supplier text, sanitized
	Area        string   `json:"area"`
	Stars       int      `json:"stars"`
	Rating      float64  `json:"rating"`
	StationM    *int     `json:"station_m,omitempty"`
	PerNightIDR int64    `json:"per_night_idr"`
	TotalIDR    int64    `json:"total_idr"`
	CheckIn     string   `json:"check_in"`
	CheckOut    string   `json:"check_out"`
	Sandbox     bool     `json:"sandbox"`
	UsedBy      []string `json:"used_by"`
}

// ActivityCard is an attraction.
type ActivityCard struct {
	ID           string   `json:"id"`
	Label        string   `json:"label"`
	PerPersonIDR int64    `json:"per_person_idr"`
	Tags         []string `json:"tags"`
	Area         string   `json:"area,omitempty"`
}

// ItineraryDay is one day on the canvas.
type ItineraryDay struct {
	DayIndex   int      `json:"day_index"`
	Date       string   `json:"date,omitempty"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary,omitempty"`
	Area       string   `json:"area,omitempty"`
	Activities []string `json:"activities"`
}

// Bar gives display proportions so the client does no money arithmetic.
type Bar struct {
	LivePct      int `json:"live_pct"`
	EstimatePct  int `json:"estimate_pct"`
	BufferPct    int `json:"buffer_pct"`
	RemainingPct int `json:"remaining_pct"`
	OverPct      int `json:"over_pct"`
}

// PackageCard is a package on the comparison board.
type PackageCard struct {
	ID           string               `json:"id"`
	Tier         string               `json:"tier"`
	Title        string               `json:"title"`
	Status       string               `json:"status"`
	Feasibility  string               `json:"feasibility"`
	Draft        bool                 `json:"draft"` // preview from build_packages, not yet saved
	Total        domain.Money         `json:"total"`
	Live         domain.Money         `json:"live"`
	Estimate     domain.Money         `json:"estimate"`
	Buffer       domain.Money         `json:"buffer"`
	Remaining    domain.Money         `json:"remaining"`
	Shortfall    domain.Money         `json:"shortfall"`
	Budget       *domain.Money        `json:"budget,omitempty"`
	Bar          Bar                  `json:"bar"`
	Nights       int                  `json:"nights"`
	Flight       string               `json:"flight"`
	Stay         string               `json:"stay"`
	Recommended  bool                 `json:"recommended"`
	Alternatives []domain.Alternative `json:"alternatives"`
	Sandbox      bool                 `json:"sandbox"`
}

// Suggestion is an actionable next step shown as a button; clicking it
// sends the Action back as a structured request.
type Suggestion struct {
	ID     string         `json:"id"`
	Label  string         `json:"label"`
	Action map[string]any `json:"action"`
}

// WSState is the whole canvas.
type WSState struct {
	Run            map[string]any                   `json:"run"`
	Phase          map[string]any                   `json:"phase"`
	Effort         map[string]agent.Decision        `json:"effort"`
	Trip           domain.TripState                 `json:"trip"`
	Flights        map[string]any                   `json:"flights"`
	Stays          map[string]any                   `json:"stays"`
	Activities     []ActivityCard                   `json:"activities"`
	Itinerary      []ItineraryDay                   `json:"itinerary"`
	Packages       []PackageCard                    `json:"packages"`
	Destinations   []priceindex.DestinationEstimate `json:"destinations"`
	Recommendation map[string]any                   `json:"recommendation"`
	Suggestions    []Suggestion                     `json:"suggestions"`
	Notices        []string                         `json:"notices"`
	Clarification  map[string]any                   `json:"clarification"`
}

type workspace struct {
	mu      sync.Mutex
	phases  []string
	focused string
}

type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

func (r *Runner) wsSet(ctx context.Context, st *runState, ops ...patchOp) {
	st.emit(ctx, "state.patch", true, "workspace", map[string]any{"ops": ops}, nil)
}

func set(path string, v any) patchOp { return patchOp{Op: "replace", Path: path, Value: v} }

func (r *Runner) wsFocus(ctx context.Context, st *runState, view, reason string) {
	st.ws.mu.Lock()
	same := st.ws.focused == view
	st.ws.focused = view
	st.ws.mu.Unlock()
	if same {
		return
	}
	st.emit(ctx, "ui.action", true, "workspace", map[string]any{"action": "focus", "view": view, "reason": reason}, nil)
}

// wsReset sends the starting snapshot: the current trip and the packages
// (with their flights, stays and itinerary) the conversation already has,
// so a non-planning turn still shows the plan.
func (r *Runner) wsReset(ctx context.Context, st *runState) {
	s := WSState{
		Run:    map[string]any{"id": st.in.RunID, "status": "running", "mode": nil},
		Phase:  map[string]any{"current": "understanding", "label": phaseLabel("understanding", st.lang()), "history": []string{}},
		Effort: map[string]agent.Decision{}, Trip: st.state,
		Flights: map[string]any{"status": "idle", "items": []FlightCard{}}, Stays: map[string]any{"status": "idle", "items": []StayCard{}},
		Activities: []ActivityCard{}, Itinerary: []ItineraryDay{}, Packages: []PackageCard{},
		Destinations: []priceindex.DestinationEstimate{}, Suggestions: []Suggestion{}, Notices: []string{},
	}
	q := r.d.DB.Q()
	if list, err := q.ListPackages(ctx, st.in.ConversationID, 6); err == nil && len(list) > 0 {
		latest := list[0].CreatedAt
		var full []domain.Package
		for _, p := range list {
			if p.Status == domain.PackageStale || latest.Sub(p.CreatedAt) > time.Minute {
				continue
			}
			if fp, _, err := q.GetPackage(ctx, p.ID); err == nil {
				full = append(full, fp)
			}
		}
		sort.Slice(full, func(i, j int) bool { return tierRank(full[i].Tier) < tierRank(full[j].Tier) })
		if len(full) > 0 {
			rec := recommend(full, "")
			s.Packages = packageCards(full, rec, false)
			s.Recommendation = map[string]any{"tier": rec, "source": "code"}
			s.Suggestions = suggestions(full, rec, st.state)
			s.Itinerary = itineraryFromPackage(pick(full, rec))
			s.Flights = map[string]any{"status": "ready", "items": flightCardsFromPackages(full)}
			s.Stays = map[string]any{"status": "ready", "items": stayCardsFromPackages(full)}
		}
	}
	st.emit(ctx, "state.reset", true, "workspace", map[string]any{"state": s}, nil)
}

// wsOnEvent projects pipeline events onto the canvas.
func (r *Runner) wsOnEvent(ctx context.Context, st *runState, typ string, payload any) {
	switch typ {
	case "status":
		p, _ := payload.(map[string]string)
		st.ws.mu.Lock()
		st.ws.phases = append(st.ws.phases, p["phase"])
		hist := append([]string{}, st.ws.phases...)
		st.ws.mu.Unlock()
		r.wsSet(ctx, st, set("/phase", map[string]any{"current": p["phase"], "label": p["label"], "history": hist}))
		switch p["phase"] {
		case "searching_flights":
			r.wsSet(ctx, st, set("/flights/status", "searching"))
			r.wsFocus(ctx, st, "flights", "Kora sedang mencari penerbangan")
		case "searching_stays":
			r.wsSet(ctx, st, set("/stays/status", "searching"))
		case "estimating":
			r.wsFocus(ctx, st, "destinations", "Membandingkan destinasi")
		}
	case "run.started":
		if m, ok := payload.(map[string]any); ok {
			if _, has := m["mode"]; has {
				r.wsSet(ctx, st, set("/run/mode", m["mode"]))
			}
		}
	case "effort.decided":
		if d, ok := payload.(agent.Decision); ok {
			r.wsSet(ctx, st, set("/effort/"+d.Role, d))
		}
	case "trip_state.updated":
		if m, ok := payload.(map[string]any); ok {
			r.wsSet(ctx, st, set("/trip", m["trip_state"]))
		}
	case "assumptions":
		r.wsFocus(ctx, st, "brief", "Asumsi yang dipakai")
	case "clarification":
		r.wsSet(ctx, st, set("/clarification", payload))
	case "notice":
		if m, ok := payload.(map[string]any); ok {
			r.wsSet(ctx, st, patchOp{Op: "add", Path: "/notices/-", Value: m["text"]})
		}
	case "tool.finished":
		m, _ := payload.(map[string]any)
		r.wsFromTool(ctx, st, m["tool"])
	case "packages.ready":
		r.wsFinalPackages(ctx, st)
	case "destinations.ready":
		r.wsSet(ctx, st, set("/destinations", st.dests))
		r.wsFocus(ctx, st, "destinations", "Perkiraan per destinasi")
	case "run.completed", "run.failed":
		status := "completed"
		if typ == "run.failed" {
			status = "failed"
		}
		r.wsSet(ctx, st, set("/run/status", status), set("/phase/current", "done"))
	}
}

// wsFromTool refreshes the canvas part a tool changed.
func (r *Runner) wsFromTool(ctx context.Context, st *runState, tool any) {
	rc := st.rc
	if rc == nil {
		return
	}
	switch tool {
	case "search_flights":
		rc.Lock()
		cards := flightCards(rc.Flights)
		est := rc.FlightEstimate
		rc.Unlock()
		status := "ready"
		value := map[string]any{"status": status, "items": cards}
		if len(cards) == 0 && est != nil {
			value["status"] = "estimate"
			value["estimate"] = map[string]any{"median_idr_per_adult": est.MedianIDR, "month": est.Month.String(), "note": est.Note}
		} else if len(cards) == 0 {
			value["status"] = "empty"
		}
		r.wsSet(ctx, st, set("/flights", value))
		r.wsFocus(ctx, st, "flights", "Kandidat penerbangan")
	case "search_stays":
		rc.Lock()
		cards := stayCards(rc.Stays)
		rc.Unlock()
		status := "ready"
		if len(cards) == 0 {
			status = "empty"
		}
		r.wsSet(ctx, st, set("/stays", map[string]any{"status": status, "items": cards}))
		r.wsFocus(ctx, st, "stays", "Kandidat penginapan")
	case "search_activities":
		rc.Lock()
		var acts []ActivityCard
		for _, n := range rc.Norms.Attractions {
			if idr, _, err := rc.FX.ToIDR(n.Amount); err == nil {
				area, _ := n.Details["area"].(string)
				acts = append(acts, ActivityCard{ID: n.ID.String(), Label: n.Label, PerPersonIDR: idr, Tags: n.Tags(), Area: area})
			}
		}
		rc.Unlock()
		sort.Slice(acts, func(i, j int) bool { return acts[i].Label < acts[j].Label })
		r.wsSet(ctx, st, set("/activities", acts))
	case "propose_day_plan":
		rc.Lock()
		days := itineraryFromPlan(rc)
		rc.Unlock()
		r.wsSet(ctx, st, set("/itinerary", days))
		r.wsFocus(ctx, st, "itinerary", "Rencana harian disusun")
	case "build_packages":
		rc.Lock()
		var prev []domain.Package
		if rc.Preview != nil {
			prev = append(prev, rc.Preview.Packages...)
		}
		rc.Unlock()
		if len(prev) > 0 {
			rec := recommend(prev, presentedTier(st))
			r.wsSet(ctx, st, set("/packages", packageCards(prev, rec, true)))
			r.wsFocus(ctx, st, "packages", "Paket sedang disusun")
		}
	case "present_to_user":
		rc.Lock()
		p := rc.Presentation
		rc.Unlock()
		if p != nil && p.View != "" {
			r.wsFocus(ctx, st, p.View, p.Reason)
		}
	}
}

// wsFinalPackages shows the saved packages, the recommendation and the next
// actions, and points the user at the recommended one.
func (r *Runner) wsFinalPackages(ctx context.Context, st *runState) {
	if len(st.packages) == 0 {
		return
	}
	rec := recommend(st.packages, presentedTier(st))
	source := "code"
	reason := ""
	if st.rc != nil {
		st.rc.Lock()
		if p := st.rc.Presentation; p != nil && p.Tier == rec {
			source, reason = "planner", p.Reason
		}
		st.rc.Unlock()
	}
	r.wsSet(ctx, st,
		set("/packages", packageCards(st.packages, rec, false)),
		set("/recommendation", map[string]any{"tier": rec, "source": source, "reason": reason}),
		set("/suggestions", suggestions(st.packages, rec, st.state)),
		set("/itinerary", itineraryFromPackage(pick(st.packages, rec))),
	)
	// Flights/stays panels mark which tier uses which candidate.
	if st.rc != nil {
		st.rc.Lock()
		f, s := flightCards(st.rc.Flights), stayCards(st.rc.Stays)
		st.rc.Unlock()
		markUsed(f, s, st.packages)
		if len(f) > 0 {
			r.wsSet(ctx, st, set("/flights/items", f))
		}
		if len(s) > 0 {
			r.wsSet(ctx, st, set("/stays/items", s))
		}
	}
	r.wsFocus(ctx, st, "packages", "Paket siap dibandingkan")
	if p := pick(st.packages, rec); p != nil {
		st.emit(ctx, "ui.action", true, "workspace", map[string]any{"action": "highlight", "target": map[string]any{"kind": "package", "id": p.ID, "tier": rec}}, nil)
	}
}

func presentedTier(st *runState) string {
	if st.rc == nil {
		return ""
	}
	st.rc.Lock()
	defer st.rc.Unlock()
	if st.rc.Presentation != nil {
		return st.rc.Presentation.Tier
	}
	return ""
}

func tierRank(t domain.Tier) int {
	for i, x := range domain.Tiers {
		if x == t {
			return i
		}
	}
	return 9
}

// recommend picks the tier to highlight: the planner's choice when it is a
// package that fits, otherwise Seimbang, then Hemat, among those that fit.
//
// Packages that fit with the buffer beat "tight" ones: a recommendation
// that leaves no contingency is not a safe default.
func recommend(pkgs []domain.Package, preferred string) string {
	comfortable := func(p domain.Package) bool {
		return p.Feasibility == domain.FeasibilityFits || p.Feasibility == domain.FeasibilityUnbounded
	}
	ok := func(p domain.Package) bool { return p.Feasibility != domain.FeasibilityOverBudget }
	for _, p := range pkgs {
		if string(p.Tier) == preferred && ok(p) {
			return preferred
		}
	}
	order := []domain.Tier{domain.TierSeimbang, domain.TierHemat, domain.TierNyaman}
	for _, accept := range []func(domain.Package) bool{comfortable, ok} {
		for _, t := range order {
			for _, p := range pkgs {
				if p.Tier == t && accept(p) {
					return string(t)
				}
			}
		}
	}
	if len(pkgs) > 0 {
		return string(pkgs[0].Tier)
	}
	return ""
}

func pick(pkgs []domain.Package, tier string) *domain.Package {
	for i := range pkgs {
		if string(pkgs[i].Tier) == tier {
			return &pkgs[i]
		}
	}
	if len(pkgs) > 0 {
		return &pkgs[0]
	}
	return nil
}

func pct(v, of int64) int {
	if of <= 0 || v <= 0 {
		return 0
	}
	return int(v * 100 / of)
}

func packageCards(pkgs []domain.Package, rec string, draft bool) []PackageCard {
	out := make([]PackageCard, 0, len(pkgs))
	for _, p := range pkgs {
		c := PackageCard{
			ID: p.ID.String(), Tier: string(p.Tier), Title: p.Title, Status: string(p.Status), Feasibility: string(p.Feasibility),
			Draft: draft, Total: domain.IDR(p.TotalIDR), Live: domain.IDR(p.LiveIDR), Estimate: domain.IDR(p.EstimateIDR),
			Buffer: domain.IDR(p.BufferIDR), Remaining: domain.IDR(p.RemainingIDR()), Shortfall: domain.IDR(p.ShortfallIDR),
			Nights: max(0, len(p.Days)-1), Recommended: string(p.Tier) == rec, Alternatives: p.Alternatives,
		}
		if c.Alternatives == nil {
			c.Alternatives = []domain.Alternative{}
		}
		scale := p.TotalIDR + p.BufferIDR
		if p.BudgetIDR != nil {
			b := domain.IDR(*p.BudgetIDR)
			c.Budget = &b
			if *p.BudgetIDR > scale {
				scale = *p.BudgetIDR
			}
		}
		c.Bar = Bar{LivePct: pct(p.LiveIDR, scale), EstimatePct: pct(p.EstimateIDR, scale), BufferPct: pct(p.BufferIDR, scale), RemainingPct: pct(p.RemainingIDR(), scale)}
		if p.BudgetIDR != nil && p.TotalIDR+p.BufferIDR > *p.BudgetIDR {
			c.Bar.OverPct = pct(p.TotalIDR+p.BufferIDR-*p.BudgetIDR, scale)
		}
		for _, it := range p.Items {
			switch it.Category {
			case domain.CatFlight:
				c.Flight = it.Title
			case domain.CatStay:
				c.Stay = it.Title
			}
			if strings.HasPrefix(it.SourceLabel, "Sandbox") {
				c.Sandbox = true
			}
		}
		out = append(out, c)
	}
	return out
}

// suggestions turns the builder's alternatives and common refinements into
// buttons that send structured actions (see conversation.PostAction).
func suggestions(pkgs []domain.Package, rec string, s domain.TripState) []Suggestion {
	var out []Suggestion
	add := func(id, label string, action map[string]any) {
		out = append(out, Suggestion{ID: id, Label: label, Action: action})
	}
	if p := pick(pkgs, rec); p != nil {
		for _, a := range p.Alternatives {
			switch a.Kind {
			case "fewer_nights":
				add("alt-nights", a.Label, map[string]any{"type": "set_slot", "payload": map[string]any{"nights": a.Value}})
			case "shift_dates":
				if v, ok := a.Value.(map[string]string); ok {
					add("alt-dates", a.Label, map[string]any{"type": "shift_dates", "payload": map[string]any{"arrival_date": v["arrival_date"]}})
				}
			case "cheaper_area":
				add("alt-area", a.Label, map[string]any{"type": "prefer_area", "payload": map[string]any{"area": a.Value}})
			case "other_destination":
				add("alt-dest", a.Label, map[string]any{"type": "set_slot", "payload": map[string]any{"destination": a.Value}})
			}
		}
	}
	if s.Nights != nil && *s.Nights < 14 {
		add("more-night", "Tambah 1 malam", map[string]any{"type": "set_slot", "payload": map[string]any{"nights": *s.Nights + 1}})
	}
	if s.Nights != nil && *s.Nights > 2 {
		add("less-night", "Kurangi 1 malam", map[string]any{"type": "set_slot", "payload": map[string]any{"nights": *s.Nights - 1}})
	}
	add("cheaper", "Cari opsi lebih hemat", map[string]any{"type": "tier_shift", "payload": map[string]any{"direction": "cheaper"}})
	add("station", "Hotel dekat stasiun", map[string]any{"type": "near_station", "payload": map[string]any{}})
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

func flightCards(m map[uuid.UUID]domain.FlightOffer) []FlightCard {
	var out []FlightCard
	for _, o := range m {
		if len(o.Outbound) == 0 || len(o.Inbound) == 0 {
			continue
		}
		out = append(out, FlightCard{
			ID: o.ID.String(), Carriers: o.Carriers(),
			Depart:      o.Outbound[0].DepartAt.Format("2006-01-02 15:04") + " " + o.Outbound[0].From,
			Arrive:      o.Outbound[len(o.Outbound)-1].ArriveAt.Format("2006-01-02 15:04") + " " + o.Outbound[len(o.Outbound)-1].To,
			ReturnDep:   o.Inbound[0].DepartAt.Format("2006-01-02 15:04") + " " + o.Inbound[0].From,
			ReturnArr:   o.Inbound[len(o.Inbound)-1].ArriveAt.Format("2006-01-02 15:04") + " " + o.Inbound[len(o.Inbound)-1].To,
			ArrivalDate: o.ArrivalDate().String(), Stops: o.Stops, Hours: float64(int(o.Duration.Hours()*10)) / 10,
			BaggageKg: o.Baggage.CheckedKg, PriceIDR: o.PriceIDR, Kind: "live", Sandbox: o.Supplier == "sandbox", UsedBy: []string{},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PriceIDR < out[j].PriceIDR })
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func stayCards(m map[uuid.UUID]domain.StayOffer) []StayCard {
	var out []StayCard
	for _, o := range m {
		n := int64(max(1, o.Nights()))
		out = append(out, StayCard{
			ID: o.ID.String(), Name: cleanName(o.Name), Area: o.Area, Stars: o.Stars, Rating: o.Rating, StationM: o.DistanceToStationM,
			PerNightIDR: o.PriceIDR / n, TotalIDR: o.PriceIDR, CheckIn: o.CheckIn.String(), CheckOut: o.CheckOut.String(),
			Sandbox: o.Supplier == "sandbox", UsedBy: []string{},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TotalIDR < out[j].TotalIDR })
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

func cleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '<' || r == '>' || r == '`' {
			return -1
		}
		return r
	}, s)
	if len([]rune(s)) > 60 {
		s = string([]rune(s)[:60]) + "…"
	}
	return s
}

func markUsed(f []FlightCard, s []StayCard, pkgs []domain.Package) {
	for _, p := range pkgs {
		for i := range f {
			if p.FlightOfferID != nil && f[i].ID == p.FlightOfferID.String() {
				f[i].UsedBy = append(f[i].UsedBy, string(p.Tier))
			}
		}
		for i := range s {
			if p.StayOfferID != nil && s[i].ID == p.StayOfferID.String() {
				s[i].UsedBy = append(s[i].UsedBy, string(p.Tier))
			}
		}
	}
}

func itineraryFromPlan(rc *agent.RunContext) []ItineraryDay {
	if rc.DayPlan == nil {
		return []ItineraryDay{}
	}
	var out []ItineraryDay
	for _, d := range rc.DayPlan.Days {
		day := ItineraryDay{DayIndex: d.DayIndex, Title: d.Title, Summary: d.Summary, Area: d.Area, Activities: []string{}}
		for _, id := range d.NormIDs {
			if n, ok := rc.Norms.Attractions[id]; ok {
				day.Activities = append(day.Activities, n.Label)
			}
		}
		out = append(out, day)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DayIndex < out[j].DayIndex })
	return out
}

func itineraryFromPackage(p *domain.Package) []ItineraryDay {
	if p == nil {
		return []ItineraryDay{}
	}
	out := make([]ItineraryDay, 0, len(p.Days))
	for _, d := range p.Days {
		day := ItineraryDay{DayIndex: d.DayIndex, Date: d.Date.String(), Title: d.Title, Summary: d.Summary, Area: d.Area, Activities: []string{}}
		for _, it := range p.Items {
			if it.Category == domain.CatActivity && it.DayIndex != nil && *it.DayIndex == d.DayIndex {
				day.Activities = append(day.Activities, it.Title)
			}
		}
		out = append(out, day)
	}
	return out
}

func flightCardsFromPackages(pkgs []domain.Package) []FlightCard {
	seen := map[string]int{}
	var out []FlightCard
	for _, p := range pkgs {
		for _, it := range p.Items {
			if it.Category != domain.CatFlight {
				continue
			}
			key := it.Title
			if i, ok := seen[key]; ok {
				out[i].UsedBy = append(out[i].UsedBy, string(p.Tier))
				continue
			}
			seen[key] = len(out)
			c := FlightCard{ID: it.ID.String(), Carriers: []string{it.Title}, PriceIDR: it.AmountIDR, Kind: string(it.PriceKind),
				Sandbox: strings.HasPrefix(it.SourceLabel, "Sandbox"), UsedBy: []string{string(p.Tier)}}
			if v, ok := it.Details["arrival_date"].(string); ok {
				c.ArrivalDate = v
			}
			if v, ok := it.Details["stops"].(float64); ok {
				c.Stops = int(v)
			}
			out = append(out, c)
		}
	}
	return out
}

func stayCardsFromPackages(pkgs []domain.Package) []StayCard {
	seen := map[string]int{}
	var out []StayCard
	for _, p := range pkgs {
		for _, it := range p.Items {
			if it.Category != domain.CatStay {
				continue
			}
			if i, ok := seen[it.Title]; ok {
				out[i].UsedBy = append(out[i].UsedBy, string(p.Tier))
				continue
			}
			seen[it.Title] = len(out)
			c := StayCard{ID: it.ID.String(), Name: cleanName(it.Title), TotalIDR: it.AmountIDR, Sandbox: strings.HasPrefix(it.SourceLabel, "Sandbox"), UsedBy: []string{string(p.Tier)}}
			if v, ok := it.Details["area"].(string); ok {
				c.Area = v
			}
			if v, ok := it.Details["stars"].(float64); ok {
				c.Stars = int(v)
			}
			if v, ok := it.Details["rating"].(float64); ok {
				c.Rating = v
			}
			if v, ok := it.Details["check_in"].(string); ok {
				c.CheckIn = v
			}
			if v, ok := it.Details["check_out"].(string); ok {
				c.CheckOut = v
			}
			if v, ok := it.Details["price_per_night_idr"].(float64); ok {
				c.PerNightIDR = int64(v)
			}
			out = append(out, c)
		}
	}
	return out
}
