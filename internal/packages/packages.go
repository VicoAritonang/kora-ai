// Package packages reads packages, selects one, re-prices live items and
// hands out outbound links (MODULE_SPEC §12).
package packages

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/costnorm"
	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/packagebuilder"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
	"github.com/koraai/koraai/internal/reference"
	"github.com/koraai/koraai/internal/store/postgres"
	"github.com/koraai/koraai/internal/usage"
	"github.com/koraai/koraai/internal/verifier"
)

// Errors mapped by the transport.
var (
	ErrNotFound       = errors.New("not found")
	ErrOfferExpired   = errors.New("offer expired")
	ErrNoOutbound     = errors.New("no outbound link")
	ErrUpstream       = errors.New("upstream error")
	ErrRateLimited    = errors.New("rate limited")
	ErrNotRepriceable = errors.New("package cannot be re-priced")
)

// Service is the packages module.
type Service struct {
	DB      *postgres.DB
	Flights ports.FlightSupplier
	Stays   ports.StaySupplier
	Norms   *costnorm.Service
	FX      *costnorm.FX
	Limiter *usage.Limiter
	Clock   platform.Clock
	Policy  packagebuilder.Policy
	FXMaxAgeDays int
}

// Get returns a package the user owns (404 otherwise).
func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (domain.Package, postgres.PackageMeta, error) {
	p, m, err := s.DB.Q().GetPackage(ctx, id)
	if errors.Is(err, postgres.ErrNotFound) || (err == nil && m.UserID != userID) {
		return p, m, ErrNotFound
	}
	return p, m, err
}

// List returns a conversation's packages.
func (s *Service) List(ctx context.Context, userID, convID uuid.UUID, limit int) ([]domain.Package, error) {
	if _, err := s.DB.Q().GetConversation(ctx, userID, convID); err != nil {
		return nil, ErrNotFound
	}
	return s.DB.Q().ListPackages(ctx, convID, limit)
}

// Select marks a package as chosen.
func (s *Service) Select(ctx context.Context, userID, id uuid.UUID) (domain.Package, error) {
	p, m, err := s.Get(ctx, userID, id)
	if err != nil {
		return p, err
	}
	if err := s.DB.WithTx(ctx, func(q *postgres.Q) error { return q.SelectPackage(ctx, m.ConversationID, id) }); err != nil {
		return p, err
	}
	p.Status = domain.PackageSelected
	return p, nil
}

// Change is one re-priced item.
type Change struct {
	ItemID   uuid.UUID    `json:"item_id"`
	Previous domain.Money `json:"previous"`
	Current  domain.Money `json:"current"`
	Status   string       `json:"status"` // unchanged | changed | unavailable
}

// RepriceResult is the reprice response.
type RepriceResult struct {
	PackageID     uuid.UUID          `json:"package_id"`
	NewPackageID  *uuid.UUID         `json:"new_package_id"`
	Changes       []Change           `json:"changes"`
	TotalPrevious domain.Money       `json:"total_previous"`
	TotalCurrent  domain.Money       `json:"total_current"`
	Feasibility   domain.Feasibility `json:"feasibility"`
}

// Reprice refreshes every live item with its supplier, rebuilds the same
// combination with the package builder, stores it as a new package and
// marks the old one stale (FR-12).
func (s *Service) Reprice(ctx context.Context, userID, id uuid.UUID) (RepriceResult, error) {
	old, meta, err := s.Get(ctx, userID, id)
	if err != nil {
		return RepriceResult{}, err
	}
	if meta.RunStatus != "completed" {
		return RepriceResult{}, ErrNotRepriceable
	}
	if !s.Limiter.Allow(ctx, "reprice:"+id.String(), 1, 5*time.Minute) {
		return RepriceResult{}, ErrRateLimited
	}
	req, err := s.DB.Q().GetTripRequest(ctx, meta.TripRequestID)
	if err != nil {
		return RepriceResult{}, err
	}
	if req.Destination != nil {
		if c, ok := reference.DestinationCity(req.Destination.Code); ok {
			req.Destination.Label = c.Label
		}
	}
	res := RepriceResult{PackageID: id, TotalPrevious: domain.IDR(old.TotalIDR)}
	in := packagebuilder.Input{Request: req, Policy: s.Policy, Now: s.Clock.Now(), NewID: platform.NewID, DestinationLabel: labelOf(req)}
	unavailable := false
	for _, it := range old.Items {
		if it.PriceKind != domain.PriceLive || it.OfferID == nil {
			continue
		}
		row, err := s.DB.Q().GetOffer(ctx, *it.OfferID)
		if err != nil {
			return res, err
		}
		ch := Change{ItemID: it.ID, Previous: domain.IDR(it.AmountIDR)}
		switch row.Kind {
		case domain.OfferFlight:
			o, err := row.FlightOffer()
			if err != nil {
				return res, err
			}
			n, err := s.Flights.Refresh(ctx, o)
			if errors.Is(err, ports.ErrNotFound) {
				ch.Status, unavailable = "unavailable", true
				res.Changes = append(res.Changes, ch)
				continue
			}
			if err != nil {
				return res, fmt.Errorf("%w: %v", ErrUpstream, err)
			}
			if n.PriceIDR, n.FX, err = s.FX.Convert(ctx, n.Price); err != nil {
				return res, fmt.Errorf("%w: %v", ErrUpstream, err)
			}
			n.SearchKey = o.SearchKey
			if err := s.DB.Q().SaveFlightOffer(ctx, n); err != nil {
				return res, err
			}
			in.Flights = append(in.Flights, n)
			ch.Current = domain.IDR(n.PriceIDR)
		case domain.OfferStay:
			o, err := row.StayOffer()
			if err != nil {
				return res, err
			}
			n, err := s.Stays.Refresh(ctx, o)
			if errors.Is(err, ports.ErrNotFound) {
				ch.Status, unavailable = "unavailable", true
				res.Changes = append(res.Changes, ch)
				continue
			}
			if err != nil {
				return res, fmt.Errorf("%w: %v", ErrUpstream, err)
			}
			if n.PriceIDR, n.FX, err = s.FX.Convert(ctx, n.PriceTotal); err != nil {
				return res, fmt.Errorf("%w: %v", ErrUpstream, err)
			}
			n.SearchKey = o.SearchKey
			if err := s.DB.Q().SaveStayOffer(ctx, n); err != nil {
				return res, err
			}
			in.Stays = append(in.Stays, n)
			ch.Current = domain.IDR(n.PriceIDR)
		default:
			continue
		}
		ch.Status = "unchanged"
		if ch.Current != ch.Previous {
			ch.Status = "changed"
		}
		res.Changes = append(res.Changes, ch)
	}
	if unavailable {
		res.TotalCurrent, res.Feasibility = res.TotalPrevious, old.Feasibility
		return res, nil // the user is told to rebuild; no new package
	}
	// Same norms and day plan as before (activities by norm id).
	if req.Destination == nil {
		return res, ErrNotRepriceable
	}
	norms, valid, err := s.Norms.ForTrip(ctx, req.Destination.Code, req.DepartDate)
	if err != nil {
		return res, err
	}
	in.Norms = norms
	if in.FX, err = s.FX.Table(ctx, costnorm.Currencies(valid)...); err != nil {
		return res, err
	}
	plan := &packagebuilder.DayPlan{ID: platform.NewID()}
	for _, d := range old.Days {
		pd := packagebuilder.PlanDay{DayIndex: d.DayIndex, Area: d.Area, Title: d.Title, Summary: d.Summary}
		for _, it := range old.Items {
			if it.Category == domain.CatActivity && it.NormID != nil && it.DayIndex != nil && *it.DayIndex == d.DayIndex {
				pd.NormIDs = append(pd.NormIDs, *it.NormID)
			}
		}
		plan.Days = append(plan.Days, pd)
	}
	in.DayPlan = plan
	out, err := packagebuilder.Build(in)
	if err != nil {
		return res, err
	}
	var np *domain.Package
	for i := range out.Packages {
		if out.Packages[i].Tier == old.Tier {
			np = &out.Packages[i]
		}
	}
	if np == nil {
		np = &out.Packages[0]
	}
	if v := verifier.CheckInvariants(*np, req, s.Clock.Now(), verifier.Options{FXMaxAgeDays: s.FXMaxAgeDays}); len(v) > 0 {
		return res, fmt.Errorf("re-priced package violates invariants: %v", v)
	}
	np.Tier, np.Title = old.Tier, old.Title
	err = s.DB.WithTx(ctx, func(q *postgres.Q) error {
		if err := q.InsertPackage(ctx, meta.ConversationID, meta.RunID, meta.TripRequestID, *np); err != nil {
			return err
		}
		return q.MarkPackageStale(ctx, old.ID)
	})
	if err != nil {
		return res, err
	}
	res.NewPackageID = &np.ID
	res.TotalCurrent = domain.IDR(np.TotalIDR)
	res.Feasibility = np.Feasibility
	return res, nil
}

func labelOf(r domain.TripRequest) string {
	if r.Destination != nil {
		return r.Destination.Label
	}
	return ""
}

// Outbound records a click and returns the supplier link (FR-9).
func (s *Service) Outbound(ctx context.Context, userID, pkgID, itemID uuid.UUID) (string, error) {
	p, _, err := s.Get(ctx, userID, pkgID)
	if err != nil {
		return "", err
	}
	for _, it := range p.Items {
		if it.ID != itemID {
			continue
		}
		if it.OfferID == nil {
			return "", ErrNoOutbound
		}
		row, err := s.DB.Q().GetOffer(ctx, *it.OfferID)
		if err != nil {
			return "", err
		}
		if !row.ExpiresAt.After(s.Clock.Now()) {
			return "", ErrOfferExpired
		}
		if row.Deeplink == nil || *row.Deeplink == "" {
			return "", ErrNoOutbound
		}
		uid := userID
		_ = s.DB.Q().RecordClick(ctx, platform.NewID(), &uid, it.ID, row.ID, row.Supplier)
		return *row.Deeplink, nil
	}
	return "", ErrNotFound
}
