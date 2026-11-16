package conversation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/reference"
	"github.com/koraai/koraai/internal/session"
	"github.com/koraai/koraai/internal/store/postgres"
)

// Action is a structured request from the web canvas (a button, a slider,
// an area chip) instead of free text. The server turns it into a TripState
// patch plus a user message that records what was clicked, then starts a
// run, so the agent sees intent precisely.
type Action struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
	Label   string         `json:"label,omitempty"`
}

// ErrUnknownAction is returned for unsupported action types.
var ErrUnknownAction = errors.New("unknown action")

func num(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

// actionPhrases is the wording of the message an action posts on the
// traveller's behalf, in their profile language (the router then answers in
// that language too).
type actionPhrases struct {
	nights, adults, children, budget, destination, change, shift, area, areaRequest, station, stationRequest, comfier, cheaper string
}

var phrases = map[string]actionPhrases{
	"id": {
		nights: "%d malam", adults: "%d dewasa", children: "%d anak", budget: "budget Rp %d", destination: "tujuan %s",
		change:         "Ubah rencana: %s. Tolong susun ulang paketnya.",
		shift:          "Geser tanggal: tiba di tujuan %s. Tolong susun ulang paketnya.",
		area:           "Saya mau menginap di area %s. Tolong cari penginapan di sana dan susun ulang paketnya.",
		areaRequest:    "Menginap di area %s",
		station:        "Tolong pilihkan hotel yang dekat stasiun dan susun ulang paketnya.",
		stationRequest: "Hotel dekat stasiun",
		comfier:        "Tolong carikan opsi yang lebih nyaman.",
		cheaper:        "Tolong carikan opsi yang lebih hemat.",
	},
	"en": {
		nights: "%d nights", adults: "%d adults", children: "%d children", budget: "budget IDR %d", destination: "destination %s",
		change:         "Change the plan: %s. Please rebuild the packages.",
		shift:          "Shift the dates: arrive at the destination on %s. Please rebuild the packages.",
		area:           "I'd like to stay in the %s area. Please find a stay there and rebuild the packages.",
		areaRequest:    "Stay in the %s area",
		station:        "Please choose a hotel close to a station and rebuild the packages.",
		stationRequest: "Hotel close to a station",
		comfier:        "Please find a more comfortable option.",
		cheaper:        "Please find a cheaper option.",
	},
}

// phrasesFor returns the wording for a profile locale (Indonesian by default).
func phrasesFor(locale string) actionPhrases {
	if p, ok := phrases[locale]; ok {
		return p
	}
	return phrases["id"]
}

// PostAction applies a structured action and starts a run.
func (s *Service) PostAction(ctx context.Context, user postgres.User, convID uuid.UUID, a Action, idemKey string) (PostResult, error) {
	conv, err := s.DB.Q().GetConversation(ctx, user.ID, convID)
	if err != nil {
		return PostResult{}, ErrNotFound
	}
	var patch domain.TripStatePatch
	var text string
	p := a.Payload
	say := phrasesFor(user.Locale)
	switch a.Type {
	case "set_slot":
		var parts []string
		if n, ok := num(p["nights"]); ok {
			if n < 1 || n > 60 {
				return PostResult{}, ErrValidation
			}
			patch.Nights = &n
			parts = append(parts, fmt.Sprintf(say.nights, n))
		}
		if n, ok := num(p["adults"]); ok {
			patch.Adults = &n
			parts = append(parts, fmt.Sprintf(say.adults, n))
		}
		if n, ok := num(p["children"]); ok {
			patch.Children = &n
			parts = append(parts, fmt.Sprintf(say.children, n))
		}
		if n, ok := num(p["budget_idr"]); ok && n > 0 {
			patch.Budget = &domain.Money{Amount: int64(n), Currency: "IDR"}
			parts = append(parts, fmt.Sprintf(say.budget, n))
		}
		if d, ok := p["destination"].(string); ok {
			pl, ok := reference.Destination(d)
			if !ok {
				return PostResult{}, ErrValidation
			}
			patch.Destination = &pl
			parts = append(parts, fmt.Sprintf(say.destination, pl.Label))
		}
		if len(parts) == 0 {
			return PostResult{}, ErrValidation
		}
		text = fmt.Sprintf(say.change, strings.Join(parts, ", "))
	case "shift_dates":
		arr, _ := p["arrival_date"].(string)
		d, err := domain.ParseDate(arr)
		if err != nil {
			return PostResult{}, ErrValidation
		}
		// Overnight outbound: departure is the day before arrival.
		w := domain.DateWindow{From: d.AddDays(-1)}
		patch.DateWindow = &w
		text = fmt.Sprintf(say.shift, d)
	case "prefer_area":
		area, _ := p["area"].(string)
		area = strings.TrimSpace(area)
		if area == "" || len(area) > 60 {
			return PostResult{}, ErrValidation
		}
		patch.Special = []domain.SpecialRequest{{Kind: "area", Text: fmt.Sprintf(say.areaRequest, area)}}
		text = fmt.Sprintf(say.area, area)
	case "near_station":
		patch.Special = []domain.SpecialRequest{{Kind: "area", Text: say.stationRequest}}
		text = say.station
	case "tier_shift":
		if p["direction"] == "comfier" {
			text = say.comfier
		} else {
			text = say.cheaper
		}
	case "ask":
		q, _ := p["text"].(string)
		text = strings.TrimSpace(q)
	default:
		return PostResult{}, ErrUnknownAction
	}
	if text == "" {
		return PostResult{}, ErrValidation
	}
	if !patch.Empty() {
		st := conv.TripState
		if err := st.Apply(patch); err != nil {
			return PostResult{}, errors.Join(ErrValidation, err)
		}
		if _, err := (session.StateStore{DB: s.DB}).Save(ctx, convID, st); err != nil {
			if errors.Is(err, session.ErrStateConflict) {
				return PostResult{}, ErrStateConflict
			}
			return PostResult{}, err
		}
	}
	label := a.Label
	if label == "" {
		label = text
	}
	return s.post(ctx, user, convID, text, idemKey, map[string]any{"action": map[string]any{"type": a.Type, "payload": a.Payload, "label": label}})
}

// LastRunID returns the conversation's most recent run (for workspace replay).
func (s *Service) LastRunID(ctx context.Context, convID uuid.UUID) *uuid.UUID {
	id, err := s.DB.Q().LatestRunID(ctx, convID)
	if err != nil {
		return nil
	}
	return &id
}
