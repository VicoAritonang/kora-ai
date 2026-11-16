package sandbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/koraai/koraai/internal/domain"
	"github.com/koraai/koraai/internal/platform"
	"github.com/koraai/koraai/internal/ports"
)

func sup() *Suppliers {
	s := New(platform.FixedClock{T: time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)}, 330, 30*time.Minute, time.Hour)
	s.Latency = 0
	return s
}

func TestSearch_OvernightArrivesNextDay(t *testing.T) {
	offers, err := sup().Search(context.Background(), ports.FlightQuery{Origin: "JKT", Destination: "OSA",
		DepartDate: domain.MustDate("2027-03-10"), ReturnDate: domain.MustDate("2027-03-17"), Pax: domain.Pax{Adults: 1}, MaxStops: -1})
	if err != nil || len(offers) < 4 {
		t.Fatalf("%v %d", err, len(offers))
	}
	for _, o := range offers {
		if o.Outbound[0].DepartAt.Hour() >= 21 && o.ArrivalDate() == o.DepartureDate() {
			t.Errorf("%s: late departure arrived same day", o.Carriers())
		}
		if o.ReturnDepartureDate().String() != "2027-03-17" {
			t.Errorf("return departs %s", o.ReturnDepartureDate())
		}
	}
}

func TestSearch_OutOfBookingWindow(t *testing.T) {
	_, err := sup().Search(context.Background(), ports.FlightQuery{Origin: "JKT", Destination: "OSA",
		DepartDate: domain.MustDate("2027-11-01"), ReturnDate: domain.MustDate("2027-11-07"), Pax: domain.Pax{Adults: 1}, MaxStops: -1})
	if !errors.Is(err, ports.ErrOutOfBookingWindow) {
		t.Fatalf("got %v", err)
	}
}

func TestSearch_Deterministic(t *testing.T) {
	q := ports.FlightQuery{Origin: "JKT", Destination: "OSA", DepartDate: domain.MustDate("2027-03-10"), ReturnDate: domain.MustDate("2027-03-17"), Pax: domain.Pax{Adults: 1}, MaxStops: -1}
	a, _ := sup().Search(context.Background(), q)
	b, _ := sup().Search(context.Background(), q)
	if len(a) != len(b) || a[0].Price != b[0].Price || a[0].SupplierRef != b[0].SupplierRef {
		t.Fatal("not deterministic")
	}
}

func TestSearchStays_RoomsForFamily(t *testing.T) {
	offers, err := Stays{sup()}.Search(context.Background(), ports.StayQuery{City: "OSA", CheckIn: domain.MustDate("2027-03-11"), CheckOut: domain.MustDate("2027-03-17"), Guests: domain.Pax{Adults: 2, Children: 1}})
	if err != nil || len(offers) == 0 {
		t.Fatal(err)
	}
	for _, o := range offers {
		if o.Guests != 3 || o.Nights() != 6 || o.RoomName != "2 kamar Twin" {
			t.Fatalf("%+v", o)
		}
	}
}
