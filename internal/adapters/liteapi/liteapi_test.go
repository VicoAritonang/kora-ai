package liteapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

const fixture = `{"data":[{"hotelId":"lp1","roomTypes":[
  {"offerId":"o1","rates":[{"name":"Double Room","retailRate":{"total":[{"amount":61200,"currency":"JPY"}]},"cancellationPolicies":{"refundableTag":"RFN"}}]},
  {"offerId":"o2","rates":[{"name":"Twin Room","retailRate":{"total":[{"amount":58800.4,"currency":"JPY"}]},"cancellationPolicies":{"refundableTag":"NRFN"}}]}
 ]}],
 "hotels":[{"id":"lp1","name":"Hotel Example Namba","address":"1-2-3 Namba, Chuo Ward, Osaka","rating":8.4,"stars":3}]}`

func TestSearch_PicksCheapestRate(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "k" {
			t.Errorf("missing key header")
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		fmt.Fprint(w, fixture)
	}))
	defer srv.Close()
	c := New(srv.URL, "k", 0, platform.FixedClock{T: time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)}, time.Hour)
	offers, err := c.Search(context.Background(), ports.StayQuery{City: "OSA", CheckIn: domain.MustDate("2027-03-11"), CheckOut: domain.MustDate("2027-03-17"), Guests: domain.Pax{Adults: 2}})
	if err != nil || len(offers) != 1 {
		t.Fatalf("%v %d", err, len(offers))
	}
	o := offers[0]
	if o.PriceTotal.Amount != 58801 || o.RoomName != "Twin Room" || o.Area != "Namba" || o.Guests != 2 {
		t.Fatalf("%+v", o)
	}
	if body["cityName"] != "Osaka" || body["guestNationality"] != "ID" {
		t.Fatalf("body %+v", body)
	}
}

func TestOccupancies_FamilyOfThree(t *testing.T) {
	occ := occupancies(domain.Pax{Adults: 2, Children: 1})
	if len(occ) != 2 {
		t.Fatalf("%v", occ)
	}
}
