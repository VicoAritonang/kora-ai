package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/reference"
)

// SearchFlights searches round trips for the request's party.
type SearchFlights struct{ d *Deps }

type flightArgs struct {
	Origin      string `json:"origin"`
	Destination string `json:"destination"`
	DepartDate  string `json:"depart_date"`
	ReturnDate  string `json:"return_date"`
	MaxStops    *int   `json:"max_stops,omitempty"`
	Cabin       string `json:"cabin,omitempty"`
}

// Name implements agent.Tool.
func (t *SearchFlights) Name() string { return "search_flights" }

// Spec implements agent.Tool.
func (t *SearchFlights) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name: "search_flights",
		Description: "Search round-trip flights for the traveller's party (party size comes from the trip request). " +
			"Returns offer_ids with carriers, local times, ARRIVAL DATE, stops, baggage and total IDR. " +
			"Do not use it to compare many dates: at most a few searches per run. Do not repeat identical arguments.",
		Parameters: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["origin","destination","depart_date","return_date"],
 "properties":{
  "origin":{"type":"string","description":"Origin city code from the trip request, e.g. JKT"},
  "destination":{"type":"string","description":"Destination city code, e.g. OSA"},
  "depart_date":{"type":"string","description":"YYYY-MM-DD, local departure date of the outbound flight"},
  "return_date":{"type":"string","description":"YYYY-MM-DD, local departure date of the return flight"},
  "max_stops":{"type":"integer","minimum":0,"maximum":2},
  "cabin":{"type":"string","enum":["economy","premium_economy","business"]}}}`),
	}
}

// Label implements agent.Tool.
func (t *SearchFlights) Label(args json.RawMessage, rc *agent.RunContext) string {
	var a flightArgs
	_ = json.Unmarshal(args, &a)
	o, _ := reference.OriginCity(strings.ToUpper(a.Origin))
	d, _ := reference.DestinationCity(strings.ToUpper(a.Destination))
	dep, _ := domain.ParseDate(a.DepartDate)
	ret, _ := domain.ParseDate(a.ReturnDate)
	return fmt.Sprintf("Mencari penerbangan %s–%s (%s – %s)", orCode(o.Label, a.Origin), orCode(d.Label, a.Destination), shortDate(dep), shortDate(ret))
}

func orCode(label, code string) string {
	if label != "" {
		return label
	}
	return code
}

// Execute implements agent.Tool.
func (t *SearchFlights) Execute(ctx context.Context, raw json.RawMessage, rc *agent.RunContext) (agent.ToolResult, error) {
	var a flightArgs
	if err := decode(raw, &a); err != nil {
		return agent.Fail("invalid_arguments", "Arguments do not match the schema: "+err.Error(), true), nil
	}
	req := rc.Request
	a.Origin, a.Destination = strings.ToUpper(a.Origin), strings.ToUpper(a.Destination)
	if req.Destination == nil || a.Destination != req.Destination.Code {
		return agent.Fail("invalid_arguments", "destination must be the trip request destination "+destCode(req), false), nil
	}
	if a.Origin != req.Origin.Code {
		return agent.Fail("invalid_arguments", "origin must be the trip request origin "+req.Origin.Code, false), nil
	}
	dep, err1 := domain.ParseDate(a.DepartDate)
	ret, err2 := domain.ParseDate(a.ReturnDate)
	if err1 != nil || err2 != nil || !ret.After(dep) || dep.DaysUntil(ret) > 30 {
		return agent.Fail("invalid_arguments", "Dates must be YYYY-MM-DD with return after departure (max 30 days).", true), nil
	}
	if !dateInWindow(req, dep) {
		return agent.Fail("invalid_arguments", fmt.Sprintf("depart_date must be within the trip window %s..%s", req.WindowStart, req.WindowEnd), true), nil
	}
	rc.Lock()
	if rc.Count.Flights >= t.d.Limits.FlightSearches {
		rc.Unlock()
		return agent.Fail("quota_exhausted", "Flight search quota for this run is used up. Build packages with the offers you have.", false), nil
	}
	rc.Count.Flights++
	rc.Unlock()

	maxStops := 1
	if a.MaxStops != nil {
		maxStops = *a.MaxStops
	}
	q := ports.FlightQuery{Origin: a.Origin, Destination: a.Destination, DepartDate: dep, ReturnDate: ret, Pax: req.Pax, Cabin: a.Cabin, MaxStops: maxStops}
	key := searchKey("flight", t.d.Flights.Name(), q.Origin, q.Destination, dep, ret, req.Pax.Adults, req.Pax.Children, q.Cabin, maxStops)
	now := rc.Now()

	var offers []domain.FlightOffer
	cacheHit := false
	if t.d.Store != nil {
		if cached, err := t.d.Store.CachedFlights(ctx, key, now.Add(cacheMargin)); err == nil && len(cached) > 0 {
			offers, cacheHit = cached, true
		}
	}
	if !cacheHit {
		res, err := t.d.Flights.Search(ctx, q)
		switch {
		case errors.Is(err, ports.ErrOutOfBookingWindow):
			return t.estimate(ctx, rc, raw, dep, ret, "out_of_booking_window"), nil
		case errors.Is(err, ports.ErrInvalidQuery):
			return agent.Fail("invalid_query", "The supplier rejected the query: "+trimErr(err), false), nil
		case err != nil:
			rc.AddNotice("flight_supplier_down", "Pencarian penerbangan sedang bermasalah; harga penerbangan memakai estimasi berlabel.")
			return t.estimate(ctx, rc, raw, dep, ret, "supplier_unavailable"), nil
		}
		for i := range res {
			o := &res[i]
			idr, fx, err := t.d.FX.Convert(ctx, o.Price)
			if err != nil {
				continue // kandidat ditolak: kurs tidak tersedia
			}
			o.PriceIDR, o.FX, o.SearchKey = idr, fx, key
			if t.d.Store != nil {
				if err := t.d.Store.SaveFlightOffer(ctx, *o); err != nil {
					continue
				}
			}
			offers = append(offers, *o)
		}
	}
	sort.Slice(offers, func(i, j int) bool { return offers[i].PriceIDR < offers[j].PriceIDR })
	rc.Lock()
	for _, o := range offers {
		rc.Flights[o.ID] = o
	}
	rc.Unlock()

	type row struct {
		OfferID     string   `json:"offer_id"`
		Carriers    []string `json:"carriers"`
		Depart      string   `json:"depart"`
		Arrive      string   `json:"arrive"`
		ArrivalDate string   `json:"arrival_date"`
		ReturnDep   string   `json:"return_depart"`
		ReturnDate  string   `json:"return_date"`
		Stops       int      `json:"stops"`
		DurationH   float64  `json:"total_travel_hours"`
		BaggageKg   int      `json:"checked_baggage_kg"`
		TotalIDR    int64    `json:"total_idr_all_pax"`
	}
	var rows []row
	for i, o := range offers {
		if i >= 8 {
			break
		}
		rows = append(rows, row{
			OfferID: o.ID.String(), Carriers: o.Carriers(),
			Depart:      o.Outbound[0].DepartAt.Format("2006-01-02 15:04") + " " + o.Outbound[0].From,
			Arrive:      o.Outbound[len(o.Outbound)-1].ArriveAt.Format("2006-01-02 15:04") + " " + o.Outbound[len(o.Outbound)-1].To,
			ArrivalDate: o.ArrivalDate().String(),
			ReturnDep:   o.Inbound[0].DepartAt.Format("2006-01-02 15:04") + " " + o.Inbound[0].From,
			ReturnDate:  o.ReturnDepartureDate().String(),
			Stops:       o.Stops, DurationH: float64(int(o.Duration.Hours()*10)) / 10, BaggageKg: o.Baggage.CheckedKg, TotalIDR: o.PriceIDR,
		})
	}
	out := map[string]any{"ok": true, "pax": req.Pax, "offers": rows, "supplier": t.d.Flights.Name()}
	code := ""
	if len(rows) == 0 {
		code = "no_results"
		out["ok"] = false
		out["code"] = code
		out["hint"] = "No flights for these dates. Try dates ±3 days inside the trip window, or allow one stop."
	}
	if t.d.Flights.Name() == "sandbox" {
		out["note"] = "sandbox supplier: simulated prices"
	}
	dig := digest(out)
	art := t.d.artifact(ctx, rc, t.Name(), json.RawMessage(raw), offers, dig)
	return agent.ToolResult{OK: len(rows) > 0, Code: code, Digest: dig, ArtifactID: art, CacheHit: cacheHit,
		Summary: fmt.Sprintf("%d penerbangan ditemukan", len(offers))}, nil
}

// estimate falls back to the cost index and labels the result (FR-14, NFR-12).
func (t *SearchFlights) estimate(ctx context.Context, rc *agent.RunContext, raw json.RawMessage, dep, ret domain.Date, code string) agent.ToolResult {
	req := rc.Request
	out := map[string]any{"ok": false, "code": code, "retryable": false}
	if code == "out_of_booking_window" {
		liveFrom := ret.AddDays(-t.d.Window)
		out["hint"] = "Airlines do not sell these dates yet. Do not search again; packages will use a labeled estimate."
		out["live_prices_from"] = liveFrom.String()
		rc.AddNotice("out_of_booking_window", fmt.Sprintf("Tiket untuk tanggal ini belum dijual maskapai; harga penerbangan memakai estimasi. Harga live diperkirakan tersedia mulai %s.", liveFrom))
	} else {
		out["hint"] = "Flight supplier unavailable. Do not retry; packages will use a labeled estimate."
	}
	if t.d.Index != nil && req.Destination != nil {
		if e, err := t.d.Index.FlightEstimate(ctx, req.Origin.Code, req.Destination.Code, dep.FirstOfMonth()); err == nil {
			rc.Lock()
			rc.FlightEstimate = &e
			rc.Unlock()
			out["estimate"] = map[string]any{"kind": "estimate", "median_idr_per_adult": e.MedianIDR, "month": e.Month.String(), "samples": e.SampleCount, "note": e.Note}
		} else {
			out["estimate"] = nil
			out["hint"] = out["hint"].(string) + " No index estimate exists either."
		}
	}
	dig := digest(out)
	art := t.d.artifact(ctx, rc, t.Name(), json.RawMessage(raw), out, dig)
	return agent.ToolResult{OK: false, Code: code, Digest: dig, ArtifactID: art, Summary: "Harga live belum tersedia; memakai estimasi"}
}

func destCode(r domain.TripRequest) string {
	if r.Destination == nil {
		return ""
	}
	return r.Destination.Code
}

func trimErr(err error) string {
	s := err.Error()
	if len(s) > 160 {
		return s[:160]
	}
	return s
}
