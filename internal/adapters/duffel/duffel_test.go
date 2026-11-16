package duffel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

// Shape follows Duffel's offer_requests response (eval/fixtures/duffel_offer_request.json).
const fixture = `{"data":{"offers":[{
 "id":"off_0000AAA","total_amount":"452.30","total_currency":"USD","expires_at":"2026-11-15T03:20:00Z",
 "slices":[
  {"duration":"PT7H25M","segments":[{"departing_at":"2027-03-10T22:00:00","arriving_at":"2027-03-11T07:25:00",
    "origin":{"iata_code":"CGK","time_zone":"Asia/Jakarta"},"destination":{"iata_code":"KIX","time_zone":"Asia/Tokyo"},
    "marketing_carrier":{"iata_code":"GA","name":"Garuda Indonesia"},"marketing_carrier_flight_number":"882",
    "passengers":[{"cabin_class":"economy","baggages":[{"type":"checked","quantity":1}]}]}]},
  {"duration":"PT7H20M","segments":[{"departing_at":"2027-03-17T11:00:00","arriving_at":"2027-03-17T16:20:00",
    "origin":{"iata_code":"KIX","time_zone":"Asia/Tokyo"},"destination":{"iata_code":"CGK","time_zone":"Asia/Jakarta"},
    "marketing_carrier":{"iata_code":"GA","name":"Garuda Indonesia"},"marketing_carrier_flight_number":"883",
    "passengers":[{"cabin_class":"economy","baggages":[{"type":"checked","quantity":1}]}]}]}
 ]}]}}`

func client(url string) *Client {
	return New(url, "t", platform.FixedClock{T: time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)}, 330, 30*time.Minute)
}

func TestSearch_MapsOffer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Duffel-Version") != "v2" {
			t.Errorf("missing Duffel-Version")
		}
		fmt.Fprint(w, fixture)
	}))
	defer srv.Close()
	offers, err := client(srv.URL).Search(context.Background(), ports.FlightQuery{Origin: "JKT", Destination: "OSA",
		DepartDate: domain.MustDate("2027-03-10"), ReturnDate: domain.MustDate("2027-03-17"), Pax: domain.Pax{Adults: 1}, MaxStops: 1})
	if err != nil || len(offers) != 1 {
		t.Fatalf("%v %d", err, len(offers))
	}
	o := offers[0]
	if o.Price.Amount != 45230 || o.Price.Currency != "USD" {
		t.Errorf("price %v", o.Price)
	}
	if o.ArrivalDate().String() != "2027-03-11" || o.ReturnDepartureDate().String() != "2027-03-17" {
		t.Errorf("dates %s %s", o.ArrivalDate(), o.ReturnDepartureDate())
	}
	// Supplier expiry earlier than our TTL wins.
	if o.ExpiresAt != time.Date(2026, 11, 15, 3, 20, 0, 0, time.UTC) {
		t.Errorf("expires %s", o.ExpiresAt)
	}
	if o.Stops != 0 || o.Baggage.CheckedKg != 23 {
		t.Errorf("%+v", o)
	}
}

func TestSearch_EmptyBeyondWindow_IsOutOfBookingWindow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"data":{"offers":[]}}`) }))
	defer srv.Close()
	_, err := client(srv.URL).Search(context.Background(), ports.FlightQuery{Origin: "JKT", Destination: "OSA",
		DepartDate: domain.MustDate("2027-11-01"), ReturnDate: domain.MustDate("2027-11-07"), Pax: domain.Pax{Adults: 1}})
	if !errors.Is(err, ports.ErrOutOfBookingWindow) {
		t.Fatalf("got %v", err)
	}
}

func TestSearch_Timeout_MapsToErrTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer srv.Close()
	c := client(srv.URL)
	c.HTTP.Timeout = 50 * time.Millisecond
	_, err := c.Search(context.Background(), ports.FlightQuery{Origin: "JKT", Destination: "OSA", DepartDate: domain.MustDate("2027-03-10"), ReturnDate: domain.MustDate("2027-03-17"), Pax: domain.Pax{Adults: 1}})
	if !errors.Is(err, ports.ErrTimeout) && !errors.Is(err, ports.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestParseAmount(t *testing.T) {
	m, err := ParseAmount("1234.567", "USD")
	if err != nil || m.Amount != 123457 {
		t.Fatalf("%v %v", m, err)
	}
	m, _ = ParseAmount("7200000", "IDR")
	if m.Amount != 7200000 {
		t.Fatal(m)
	}
}
