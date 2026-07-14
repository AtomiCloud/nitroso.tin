package buyer

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	openapi_types "github.com/deepmap/oapi-codegen/pkg/types"
	"github.com/rs/zerolog"
)

func TestCurrencyOrDefault(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{name: "passenger response", values: []string{"myr", "SGD"}, want: "MYR"},
		{name: "later response fallback", values: []string{"", " sgd "}, want: "SGD"},
		{name: "KTMB eWallet default", values: []string{"", "  "}, want: "MYR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := currencyOrDefault(tt.values...); got != tt.want {
				t.Errorf("currencyOrDefault(%q) = %q, want %q", tt.values, got, tt.want)
			}
		})
	}
}

func TestCompleteOnceIncludesKtmbCostMultipartFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1.0/Booking/complete/aaaaaaaa-0000-4000-8000-000000000001" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("bookingNo"); got != "B-1" {
			t.Errorf("bookingNo = %q, want B-1", got)
		}
		if got := r.URL.Query().Get("ticketNo"); got != "T-1" {
			t.Errorf("ticketNo = %q, want T-1", got)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse multipart form: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if got := r.FormValue("ktmbAmount"); got != "31.5" {
			t.Errorf("ktmbAmount = %q, want 31.5", got)
		}
		if got := r.FormValue("ktmbCurrency"); got != "MYR" {
			t.Errorf("ktmbCurrency = %q, want MYR", got)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Errorf("read file field: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil || string(content) != "ticket-pdf" {
			t.Errorf("file content = %q, %v; want ticket-pdf", content, err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	zincClient, err := zinc.NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.Nop()
	client := &Client{zinc: zincClient, logger: &logger}
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte("aaaaaaaa-0000-4000-8000-000000000001")); err != nil {
		t.Fatal(err)
	}
	if err := client.completeOnce(context.Background(), id, "B-1", "T-1", 31.5, "MYR", []byte("ticket-pdf")); err != nil {
		t.Fatalf("completeOnce returned error: %v", err)
	}
}

func TestTicketIdsOf(t *testing.T) {
	wellFormed := ktmb.CompleteRes{Booking: ktmb.CompleteBookingRes{
		BookingNo: "KB001",
		Trips: []ktmb.CompleteTripRes{
			{Tickets: []ktmb.CompleteTicketRes{{TicketNo: "TK001"}}},
		},
	}}
	bookingNo, ticketNo, err := ticketIdsOf(wellFormed)
	if err != nil || bookingNo != "KB001" || ticketNo != "TK001" {
		t.Errorf("well-formed: got (%q, %q, %v), want (KB001, TK001, nil)", bookingNo, ticketNo, err)
	}

	// a successful Complete with no trips must error (the caller parks with a
	// PurchasedError), preserving the booking number for recovery
	noTrips := ktmb.CompleteRes{Booking: ktmb.CompleteBookingRes{BookingNo: "KB002"}}
	bookingNo, ticketNo, err = ticketIdsOf(noTrips)
	if err == nil || bookingNo != "KB002" || ticketNo != "" {
		t.Errorf("no trips: got (%q, %q, %v), want (KB002, \"\", error)", bookingNo, ticketNo, err)
	}

	// trips present but no tickets: same contract
	noTickets := ktmb.CompleteRes{Booking: ktmb.CompleteBookingRes{
		BookingNo: "KB003",
		Trips:     []ktmb.CompleteTripRes{{}},
	}}
	bookingNo, ticketNo, err = ticketIdsOf(noTickets)
	if err == nil || bookingNo != "KB003" || ticketNo != "" {
		t.Errorf("no tickets: got (%q, %q, %v), want (KB003, \"\", error)", bookingNo, ticketNo, err)
	}

	// fully blank response still errors without panicking
	if _, _, err = ticketIdsOf(ktmb.CompleteRes{}); err == nil {
		t.Error("blank response: expected error, got nil")
	}
}
