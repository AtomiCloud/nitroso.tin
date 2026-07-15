package terminator

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	openapi_types "github.com/deepmap/oapi-codegen/pkg/types"
	"github.com/rs/zerolog"
)

type fakeRefundClient struct {
	policy      ktmb.GenericRes[ktmb.RefundPolicyRes]
	refund      ktmb.GenericRes[*interface{}]
	refundCalls int
	list        *ktmb.GenericRes[ktmb.TicketListRes]
	refunded    string
}

func (f *fakeRefundClient) ListTicket(string, int64) (ktmb.GenericRes[ktmb.TicketListRes], error) {
	if f.list != nil {
		return *f.list, nil
	}
	return ktmb.GenericRes[ktmb.TicketListRes]{Status: true, Data: ktmb.TicketListRes{
		TotalPage: 1,
		Bookings: []ktmb.TicketListBookingRes{{
			BookingNo:   "B-1",
			BookingData: "booking-data",
			Trips: []ktmb.TicketListTripRes{{Tickets: []ktmb.TicketListTicketRes{{
				TicketNo: "T-1", TicketData: "ticket-data",
			}}}},
		}},
	}}, nil
}

func (f *fakeRefundClient) GetRefundPolicy(string, string, string) (ktmb.GenericRes[ktmb.RefundPolicyRes], error) {
	return f.policy, nil
}

func (f *fakeRefundClient) RefundTicket(_, _, _, ticketData string) (ktmb.GenericRes[*interface{}], error) {
	f.refundCalls++
	f.refunded = ticketData
	return f.refund, nil
}

type fakeTermSession struct{}

func (*fakeTermSession) Login(context.Context, string, string) (string, error) {
	return "user-data", nil
}

type fakeReporter struct {
	status int
	err    error
	id     openapi_types.UUID
	body   zinc.PostApiVVersionBookingIdKtmbRefundJSONBody
	calls  int
}

func (f *fakeReporter) PostApiVVersionBookingIdKtmbRefund(_ context.Context, _ string, id openapi_types.UUID, body zinc.PostApiVVersionBookingIdKtmbRefundJSONBody, _ ...zinc.RequestEditorFn) (*http.Response, error) {
	f.calls++
	f.id = id
	f.body = body
	if f.err != nil {
		return nil, f.err
	}
	status := f.status
	if status == 0 {
		status = http.StatusNoContent
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
}

func terminationID(t *testing.T) openapi_types.UUID {
	t.Helper()
	var id openapi_types.UUID
	if err := id.UnmarshalText([]byte("aaaaaaaa-0000-4000-8000-000000000001")); err != nil {
		t.Fatal(err)
	}
	return id
}

func successfulKTMB() *fakeRefundClient {
	return &fakeRefundClient{
		policy: ktmb.GenericRes[ktmb.RefundPolicyRes]{Status: true, Data: ktmb.RefundPolicyRes{
			CurrencyCode:      "MYR",
			TotalRefundAmount: 99,
			Trips: []ktmb.RefundPolicyTripRes{{Tickets: []ktmb.RefundPolicyTripTicketRes{{
				TicketNo: "T-1", RefundAmount: 8.5, CurrencyCode: "myr", TicketData: "policy-ticket-data",
			}}}},
		}},
		refund: ktmb.GenericRes[*interface{}]{Status: true},
	}
}

func TestTerminateReportsPerTicketRefundAfterKTMBSuccess(t *testing.T) {
	ktmbClient := successfulKTMB()
	reporter := &fakeReporter{}
	logger := zerolog.Nop()
	term := NewTerminator(ktmbClient, &fakeTermSession{}, reporter, &logger, config.EnricherConfig{Email: "e", Password: "p"})
	id := terminationID(t)

	if err := term.Terminate(context.Background(), BookingTermination{Id: id, BookingNo: "B-1", TicketNo: "T-1"}); err != nil {
		t.Fatalf("Terminate returned error: %v", err)
	}
	if ktmbClient.refundCalls != 1 || reporter.calls != 1 || reporter.id != id {
		t.Fatalf("refund calls=%d reporter calls=%d id=%s", ktmbClient.refundCalls, reporter.calls, reporter.id)
	}
	if reporter.body.RefundAmount != 8.5 || reporter.body.RefundCurrency != "MYR" {
		t.Fatalf("reported refund = %+v, want per-ticket 8.5 MYR", reporter.body)
	}
}

func TestTerminateDoesNotFailWhenZincRefundReportFails(t *testing.T) {
	ktmbClient := successfulKTMB()
	reporter := &fakeReporter{err: errors.New("zinc unavailable")}
	logger := zerolog.Nop()
	term := NewTerminator(ktmbClient, &fakeTermSession{}, reporter, &logger, config.EnricherConfig{Email: "e", Password: "p"})

	if err := term.Terminate(context.Background(), BookingTermination{Id: terminationID(t), BookingNo: "B-1", TicketNo: "T-1"}); err != nil {
		t.Fatalf("zinc report failure must not fail termination: %v", err)
	}
	if reporter.calls != 1 {
		t.Fatalf("reporter calls=%d, want 1", reporter.calls)
	}
}

func TestTerminateRefundsTheRequestedTicketFromAMultiTicketPolicy(t *testing.T) {
	listing := ktmb.GenericRes[ktmb.TicketListRes]{Status: true, Data: ktmb.TicketListRes{
		TotalPage: 1,
		Bookings: []ktmb.TicketListBookingRes{{
			BookingNo:   "B-1",
			BookingData: "booking-data",
			Trips: []ktmb.TicketListTripRes{{Tickets: []ktmb.TicketListTicketRes{
				{TicketNo: "T-1", TicketData: "list-ticket-data-1"},
				{TicketNo: "T-2", TicketData: "list-ticket-data-2"},
			}}},
		}},
	}}
	ktmbClient := &fakeRefundClient{
		list: &listing,
		policy: ktmb.GenericRes[ktmb.RefundPolicyRes]{Status: true, Data: ktmb.RefundPolicyRes{
			BookingData:  "policy-booking-data",
			CurrencyCode: "MYR",
			Trips: []ktmb.RefundPolicyTripRes{{Tickets: []ktmb.RefundPolicyTripTicketRes{
				{TicketNo: "T-1", RefundAmount: 4, TicketData: "policy-ticket-data-1"},
				{TicketNo: "T-2", RefundAmount: 8, TicketData: "policy-ticket-data-2"},
			}}},
		}},
		refund: ktmb.GenericRes[*interface{}]{Status: true},
	}
	reporter := &fakeReporter{}
	logger := zerolog.Nop()
	term := NewTerminator(ktmbClient, &fakeTermSession{}, reporter, &logger, config.EnricherConfig{Email: "e", Password: "p"})

	if err := term.Terminate(context.Background(), BookingTermination{Id: terminationID(t), BookingNo: "B-1", TicketNo: "T-2"}); err != nil {
		t.Fatalf("Terminate returned error: %v", err)
	}
	if ktmbClient.refunded != "policy-ticket-data-2" {
		t.Fatalf("RefundTicket ticketData = %q, want requested ticket's policy-ticket-data-2", ktmbClient.refunded)
	}
	if reporter.body.RefundAmount != 8 {
		t.Fatalf("reported refund = %+v, want requested ticket's amount 8", reporter.body)
	}
}

func TestRefundAmountFallsBackToPolicyTotal(t *testing.T) {
	amount, currency, ok := refundAmount(ktmb.RefundPolicyRes{TotalRefundAmount: 12, CurrencyCode: "sgd"}, "T-1")
	if !ok || amount != 12 || currency != "SGD" {
		t.Fatalf("refundAmount = (%v, %q, %v), want (12, SGD, true)", amount, currency, ok)
	}
}

func TestRefundAmountDoesNotAssignAMultiTicketTotalToOneTicket(t *testing.T) {
	policy := ktmb.RefundPolicyRes{
		TotalRefundAmount: 12,
		CurrencyCode:      "MYR",
		Trips: []ktmb.RefundPolicyTripRes{{Tickets: []ktmb.RefundPolicyTripTicketRes{
			{TicketNo: "T-1"},
			{TicketNo: "T-2", RefundAmount: 12},
		}}},
	}

	if amount, currency, ok := refundAmount(policy, "T-1"); ok {
		t.Fatalf("refundAmount = (%v, %q, true), want no exact amount for T-1", amount, currency)
	}
}
