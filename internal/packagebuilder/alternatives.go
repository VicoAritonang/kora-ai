package packagebuilder

import (
	"fmt"
	"sort"

	"github.com/koraai/koraai/internal/domain"
)

// alternatives returns the ordered ways out when nothing fits the budget
// (MODULE_SPEC §9 "Tidak ada yang muat"). Every number is computed here from
// candidates and norms; none is invented.
func (b *builder) alternatives(p domain.Package, chosen combo, all []combo) []domain.Alternative {
	var out []domain.Alternative
	if p.BudgetIDR == nil {
		return out
	}
	budget := *p.BudgetIDR
	needed := p.TotalIDR + p.BufferIDR

	cheapestFlight := int64(-1)
	for _, c := range all {
		if cheapestFlight < 0 || c.f.priceIDR < cheapestFlight {
			cheapestFlight = c.f.priceIDR
		}
	}
	tiny := cheapestFlight >= 0 && budget < cheapestFlight

	if !tiny {
		// 1. Shift dates: the cheapest combination departing on another date.
		var best *combo
		for i := range all {
			c := all[i]
			if c.f.arrival == chosen.f.arrival {
				continue
			}
			if best == nil || c.f.priceIDR+c.s.priceIDR < best.f.priceIDR+best.s.priceIDR {
				best = &all[i]
			}
		}
		if best != nil {
			saving := (chosen.f.priceIDR + chosen.s.priceIDR) - (best.f.priceIDR + best.s.priceIDR)
			if saving > 0 {
				out = append(out, domain.Alternative{
					Kind:      "shift_dates",
					Label:     fmt.Sprintf("Geser kedatangan ke %s (hemat sekitar %s)", best.f.arrival, rupiah(saving)),
					SavingIDR: saving,
					Value:     map[string]string{"arrival_date": best.f.arrival.String(), "return_date": best.f.returnDep.String()},
				})
			}
		}

		// 2. Fewer nights: the most nights that fit, keeping fixed costs.
		if n, saving, ok := b.fewerNights(p, chosen, budget); ok {
			out = append(out, domain.Alternative{
				Kind:      "fewer_nights",
				Label:     fmt.Sprintf("Kurangi jadi %d malam (hemat sekitar %s)", n, rupiah(saving)),
				SavingIDR: saving,
				Value:     n,
			})
		}

		// 3. Cheaper area: the cheapest stay in a different area, same dates.
		var cheaper *combo
		for i := range all {
			c := all[i]
			if c.s.live == nil || c.f.id() != chosen.f.id() || c.s.area == chosen.s.area {
				continue
			}
			if cheaper == nil || c.s.priceIDR < cheaper.s.priceIDR {
				cheaper = &all[i]
			}
		}
		if cheaper != nil && cheaper.s.priceIDR < chosen.s.priceIDR {
			saving := chosen.s.priceIDR - cheaper.s.priceIDR
			out = append(out, domain.Alternative{
				Kind:      "cheaper_area",
				Label:     fmt.Sprintf("Menginap di area %s (hemat sekitar %s)", cheaper.s.area, rupiah(saving)),
				SavingIDR: saving,
				Value:     cheaper.s.area,
			})
		}
	}

	// 4. Another destination from the cost index.
	dests := append([]DestinationOption(nil), b.in.Destinations...)
	sort.Slice(dests, func(i, j int) bool { return dests[i].TotalIDR < dests[j].TotalIDR })
	for _, d := range dests {
		if b.in.Request.Destination != nil && d.Code == b.in.Request.Destination.Code {
			continue
		}
		if d.TotalIDR <= budget {
			out = append(out, domain.Alternative{
				Kind:  "other_destination",
				Label: fmt.Sprintf("Pertimbangkan %s (perkiraan total %s)", d.Label, rupiah(d.TotalIDR)),
				Value: d.Code,
			})
			break
		}
	}

	// 5. Add funds.
	if needed > budget {
		out = append(out, domain.Alternative{
			Kind:  "add_funds",
			Label: fmt.Sprintf("Butuh tambahan sekitar %s", rupiah(needed-budget)),
			Value: needed - budget,
		})
	}
	return out
}

// fewerNights finds the largest night count below the current one that fits,
// pricing the variable part (stay per night + meals + local transport) per
// night from the package itself.
func (b *builder) fewerNights(p domain.Package, c combo, budget int64) (int, int64, bool) {
	nights := c.s.nights()
	if nights <= 1 {
		return 0, 0, false
	}
	var fullDayCost int64 // meals + local transport on one middle day
	middle := 1
	for _, it := range p.Items {
		if it.DayIndex != nil && *it.DayIndex == middle && it.Category != domain.CatActivity {
			fullDayCost += it.AmountIDR
		}
	}
	perNight := c.s.perNightIDR*int64(max(1, c.s.rooms)) + fullDayCost
	for n := nights - 1; n >= 1; n-- {
		saving := int64(nights-n) * perNight
		if p.TotalIDR-saving+p.BufferIDR <= budget {
			return n, saving, true
		}
	}
	return 0, 0, false
}

// rupiah formats an amount the Indonesian way: "Rp 1.250.000".
func rupiah(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprintf("%d", v)
	var out []byte
	for i, ch := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, ch)
	}
	if neg {
		return "-Rp " + string(out)
	}
	return "Rp " + string(out)
}
