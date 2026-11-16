package pipeline

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"github.com/koraai/koraai/internal/agent"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/ports"
)

// prefetch runs the searches every plan needs before the planner starts:
// flights and activities in parallel, then stays for the most common arrival
// date (IMPLEMENTATION_PLAN §8: "pra-pencarian di pipeline"). The calls go
// through the same guard, quotas and events as the planner's own calls, and
// are handed to the planner as if it had made them, which saves two model
// round trips.
func (r *Runner) prefetch(ctx context.Context, st *runState, in agent.LoopInput) []ports.InputItem {
	req := st.req
	st.status(ctx, "searching_flights")
	type call struct {
		c   ports.ToolCall
		out string
	}
	mk := func(id, name string, args any) ports.ToolCall {
		b, _ := json.Marshal(args)
		return ports.ToolCall{CallID: id, Name: name, Arguments: b}
	}
	first := []*call{{c: mk("prefetch_flights", "search_flights", map[string]any{
		"origin": req.Origin.Code, "destination": req.Destination.Code, "depart_date": req.DepartDate.String(), "return_date": req.ReturnDate.String(),
	})}}
	if len(req.Interests) > 0 || len(req.Special) > 0 {
		var tags []string
		tags = append(tags, req.Interests...)
		for _, s := range req.Special {
			if s.Kind == "festival" || s.Kind == "children" {
				tags = append(tags, map[string]string{"festival": "festival", "children": "family"}[s.Kind])
			}
		}
		first = append(first, &call{c: mk("prefetch_activities", "search_activities", map[string]any{"city": req.Destination.Code, "interests": uniq(tags)})})
	}
	var wg sync.WaitGroup
	for _, c := range first {
		wg.Add(1)
		go func(c *call) {
			defer wg.Done()
			c.out = agent.RunToolCall(ctx, in, c.c)
		}(c)
	}
	wg.Wait()

	all := first
	// Stays for the arrival date most offers share.
	rc := st.rc
	rc.Lock()
	counts := map[[2]domain.Date]int{}
	for _, f := range rc.Flights {
		counts[[2]domain.Date{f.ArrivalDate(), f.ReturnDepartureDate()}]++
	}
	hasEstimate := rc.FlightEstimate != nil
	rc.Unlock()
	var keys [][2]domain.Date
	for k := range counts {
		keys = append(keys, k)
	}
	// Prefer the arrival group whose hotel stay equals the requested nights:
	// the return date assumes an overnight outbound, but some carriers land
	// the same day, which would silently add a night.
	exact := func(k [2]domain.Date) bool { return k[0].DaysUntil(k[1]) == req.Nights }
	sort.Slice(keys, func(i, j int) bool {
		if exact(keys[i]) != exact(keys[j]) {
			return exact(keys[i])
		}
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i][0].Before(keys[j][0])
	})
	var in1, out1 domain.Date
	switch {
	case len(keys) > 0:
		in1, out1 = keys[0][0], keys[0][1]
	case hasEstimate:
		// Out of the sales window: stays for the same dates the estimate uses.
		in1, out1 = req.ReturnDate.AddDays(-req.Nights), req.ReturnDate
	}
	if !in1.IsZero() && out1.After(in1) {
		st.status(ctx, "searching_stays")
		c := &call{c: mk("prefetch_stays", "search_stays", map[string]any{"city": req.Destination.Code, "check_in": in1.String(), "check_out": out1.String()})}
		c.out = agent.RunToolCall(ctx, in, c.c)
		all = append(all, c)
	}
	st.status(ctx, "planning")
	var work []ports.InputItem
	for _, c := range all {
		work = append(work, ports.InputItem{Type: "function_call", CallID: c.c.CallID, Name: c.c.Name, Arguments: string(c.c.Arguments)})
		work = append(work, ports.InputItem{Type: "function_call_output", CallID: c.c.CallID, Content: c.out})
	}
	return work
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
