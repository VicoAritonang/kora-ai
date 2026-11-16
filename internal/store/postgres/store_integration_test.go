//go:build integration

// Integration tests against a real Postgres with db/schema.sql applied
// (ENGINEERING_STANDARDS §5: no database mocks). Run:
//
//	TEST_DB_URL=postgres://... go test -tags integration ./internal/store/...
package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/koraai/koraai/internal/domain"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	url := os.Getenv("TEST_DB_URL")
	if url == "" {
		t.Skip("TEST_DB_URL not set")
	}
	db, err := Open(context.Background(), url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func id() uuid.UUID { u, _ := uuid.NewV7(); return u }

func TestConversationRunAndEvents(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	q := db.Q()
	u, err := q.CreateUser(ctx, id(), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	defer q.db.Exec(ctx, `delete from users where id = $1`, u.ID)
	c, err := q.CreateConversation(ctx, id(), u.ID, nil, domain.TripState{})
	if err != nil {
		t.Fatal(err)
	}
	m1, _ := q.InsertMessage(ctx, id(), c.ID, "user", "halo", nil, nil)
	m2, _ := q.InsertMessage(ctx, id(), c.ID, "user", "lagi", nil, nil)
	if m1.Seq != 1 || m2.Seq != 2 {
		t.Fatalf("seq %d %d", m1.Seq, m2.Seq)
	}
	r, err := q.InsertRun(ctx, id(), c.ID, m1.ID, map[string]any{"x": 1})
	if err != nil {
		t.Fatal(err)
	}
	// One active run per conversation (partial unique index).
	if _, err := q.InsertRun(ctx, id(), c.ID, m2.ID, map[string]any{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("second active run: %v", err)
	}
	for i := 1; i <= 3; i++ {
		vis := "internal"
		if i != 2 {
			vis = "public"
		}
		if err := q.InsertEvent(ctx, Event{RunID: r.ID, Seq: i, Type: "status", Visibility: vis, Payload: []byte(`{"i":1}`), CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	pub, _ := q.ListEvents(ctx, r.ID, 1, true, 10)
	if len(pub) != 1 || pub[0].Seq != 3 {
		t.Fatalf("replay after seq 1: %+v", pub)
	}
	if err := q.FinishRun(ctx, r.ID, "completed", nil, RunTotals{Steps: 2}); err != nil {
		t.Fatal(err)
	}
	if active, _ := q.ActiveRun(ctx, c.ID); active != nil {
		t.Fatal("run still active")
	}
}

func TestTripStateOptimisticLock(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	q := db.Q()
	u, _ := q.CreateUser(ctx, id(), nil, true)
	defer q.db.Exec(ctx, `delete from users where id = $1`, u.ID)
	c, _ := q.CreateConversation(ctx, id(), u.ID, nil, domain.TripState{})
	if _, err := q.SaveTripState(ctx, c.ID, domain.TripState{}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := q.SaveTripState(ctx, c.ID, domain.TripState{}, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale version accepted: %v", err)
	}
}

// G2 at the database level: a live item without an offer is rejected.
func TestPackageItemChecks_EnforceProvenance(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	q := db.Q()
	u, _ := q.CreateUser(ctx, id(), nil, true)
	defer q.db.Exec(ctx, `delete from users where id = $1`, u.ID)
	c, _ := q.CreateConversation(ctx, id(), u.ID, nil, domain.TripState{})
	m, _ := q.InsertMessage(ctx, id(), c.ID, "user", "x", nil, nil)
	r, _ := q.InsertRun(ctx, id(), c.ID, m.ID, map[string]any{})
	dest := domain.Place{Code: "OSA", Label: "Osaka"}
	budget := domain.IDR(1)
	req := domain.TripRequest{ID: id(), Origin: domain.Place{Code: "JKT"}, Destination: &dest, Budget: &budget, Nights: 2, Pax: domain.Pax{Adults: 1}}
	if err := q.InsertTripRequest(ctx, c.ID, r.ID, req); err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour)
	bad := domain.Package{
		ID: id(), Tier: domain.TierHemat, Title: "t", Status: domain.PackageProposed, Feasibility: domain.FeasibilityUnbounded,
		TotalIDR: 10, LiveIDR: 10, BuilderVersion: "test", CreatedAt: time.Now(),
		Items: []domain.PackageItem{{ID: id(), Category: domain.CatFlight, Title: "f", Quantity: 1, UnitAmount: 10, Currency: "IDR", AmountIDR: 10,
			PriceKind: domain.PriceLive, SourceLabel: "x", PricedAt: time.Now(), ExpiresAt: &exp}},
	}
	err := db.WithTx(ctx, func(tx *Q) error { return tx.InsertPackage(ctx, c.ID, r.ID, req.ID, bad) })
	if err == nil {
		t.Fatal("live item without offer_id was accepted")
	}
	bad.TotalIDR = 11 // total ≠ live + estimate
	bad.Items = nil
	if err := db.WithTx(ctx, func(tx *Q) error { return tx.InsertPackage(ctx, c.ID, r.ID, req.ID, bad) }); err == nil {
		t.Fatal("inconsistent totals accepted")
	}
}

func TestNormsSeeded(t *testing.T) {
	db := testDB(t)
	n, err := db.Q().ListNorms(context.Background(), "OSA")
	if err != nil {
		t.Fatal(err)
	}
	if len(n) < 20 {
		t.Fatalf("seed not applied: %d norms", len(n))
	}
}
