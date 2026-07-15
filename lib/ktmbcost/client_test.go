package ktmbcost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/AtomiCloud/nitroso-tin/lib/ktmb"
	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	openapi_types "github.com/deepmap/oapi-codegen/pkg/types"
	"github.com/rs/zerolog"
)

const (
	idA = "aaaaaaaa-0000-4000-8000-000000000001"
	idB = "aaaaaaaa-0000-4000-8000-000000000002"
	idC = "aaaaaaaa-0000-4000-8000-000000000003"
	idD = "aaaaaaaa-0000-4000-8000-000000000004"
)

type recordedCost struct {
	id   string
	body zinc.PostApiVVersionBookingIdKtmbCostJSONBody
}

type recordedRefund struct {
	id   string
	body zinc.PostApiVVersionBookingIdKtmbRefundJSONBody
}

type zincStub struct {
	t            *testing.T
	pages        map[int][]zinc.BookingKtmbCostMissingRes
	missing      []zinc.BookingKtmbCostMissingRes
	dynamic      bool
	postStatuses map[string]int
	listSkips    []int
	listStatuses []int
	posts        []recordedCost
	refunds      []recordedRefund
}

func (s *zincStub) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1.0/Booking/ktmb-cost/missing":
			status, err := strconv.Atoi(r.URL.Query().Get("Status"))
			if err != nil {
				s.t.Errorf("invalid Status %q", r.URL.Query().Get("Status"))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			s.listStatuses = append(s.listStatuses, status)
			limit, err := strconv.Atoi(r.URL.Query().Get("Limit"))
			if err != nil || limit <= 0 {
				s.t.Errorf("invalid Limit %q", r.URL.Query().Get("Limit"))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			skip, err := strconv.Atoi(r.URL.Query().Get("Skip"))
			if err != nil {
				s.t.Errorf("invalid Skip %q", r.URL.Query().Get("Skip"))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			s.listSkips = append(s.listSkips, skip)
			page := s.pages[skip]
			if s.dynamic {
				posted := make(map[string]struct{}, len(s.posts))
				for _, post := range s.posts {
					posted[post.id] = struct{}{}
				}
				remaining := make([]zinc.BookingKtmbCostMissingRes, 0, len(s.missing))
				for _, item := range s.missing {
					if _, done := posted[item.Id.String()]; !done {
						remaining = append(remaining, item)
					}
				}
				if skip < len(remaining) {
					end := min(skip+limit, len(remaining))
					page = remaining[skip:end]
				} else {
					page = nil
				}
			}
			if err := json.NewEncoder(w).Encode(page); err != nil {
				s.t.Fatal(err)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1.0/Booking/ktmb-refund/missing":
			limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
			if err != nil || limit <= 0 {
				s.t.Errorf("invalid refund limit %q", r.URL.Query().Get("limit"))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			page := s.missing
			if len(page) > limit {
				page = page[:limit]
			}
			if err := json.NewEncoder(w).Encode(page); err != nil {
				s.t.Fatal(err)
			}
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1.0/Booking/") && strings.HasSuffix(r.URL.Path, "/ktmb-cost"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1.0/Booking/"), "/ktmb-cost")
			var body zinc.PostApiVVersionBookingIdKtmbCostJSONBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				s.t.Errorf("decode post body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			s.posts = append(s.posts, recordedCost{id: id, body: body})
			if status := s.postStatuses[id]; status != 0 {
				w.WriteHeader(status)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1.0/Booking/") && strings.HasSuffix(r.URL.Path, "/ktmb-refund"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1.0/Booking/"), "/ktmb-refund")
			var body zinc.PostApiVVersionBookingIdKtmbRefundJSONBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				s.t.Errorf("decode refund body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			s.refunds = append(s.refunds, recordedRefund{id: id, body: body})
			if status := s.postStatuses[id]; status != 0 {
				w.WriteHeader(status)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			s.t.Errorf("unexpected zinc call: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

type fakeTickets struct {
	results     map[string]ktmb.GenericRes[ktmb.GetTicketRes]
	sequences   map[string][]ktmb.GenericRes[ktmb.GetTicketRes]
	errors      map[string]error
	calls       []string
	userData    []string
	rawResults  map[string]ktmb.GenericRes[json.RawMessage]
	policies    map[string]ktmb.GenericRes[ktmb.RefundPolicyRes]
	policyCalls []string
}

func (f *fakeTickets) GetTicket(userData, bookingNo, _ string) (ktmb.GenericRes[ktmb.GetTicketRes], error) {
	f.calls = append(f.calls, bookingNo)
	f.userData = append(f.userData, userData)
	if err := f.errors[bookingNo]; err != nil {
		return ktmb.GenericRes[ktmb.GetTicketRes]{}, err
	}
	if sequence := f.sequences[bookingNo]; len(sequence) > 0 {
		result := sequence[0]
		f.sequences[bookingNo] = sequence[1:]
		return result, nil
	}
	return f.results[bookingNo], nil
}

func (f *fakeTickets) GetTicketRaw(userData, bookingNo, _ string) (ktmb.GenericRes[json.RawMessage], error) {
	f.calls = append(f.calls, bookingNo)
	f.userData = append(f.userData, userData)
	if err := f.errors[bookingNo]; err != nil {
		return ktmb.GenericRes[json.RawMessage]{}, err
	}
	return f.rawResults[bookingNo], nil
}

func (f *fakeTickets) GetRefundPolicy(_ string, bookingData, _ string) (ktmb.GenericRes[ktmb.RefundPolicyRes], error) {
	f.policyCalls = append(f.policyCalls, bookingData)
	return f.policies[bookingData], nil
}

type fakeSession struct {
	calls         int
	err           error
	tokens        []string
	invalidations []string
	invalidateErr error
}

func (s *fakeSession) Login(context.Context, string, string) (string, error) {
	s.calls++
	if len(s.tokens) > 0 {
		index := min(s.calls-1, len(s.tokens)-1)
		return s.tokens[index], s.err
	}
	return "user-data", s.err
}

func (s *fakeSession) Invalidate(_ context.Context, userData string) error {
	s.invalidations = append(s.invalidations, userData)
	return s.invalidateErr
}

func workItem(t *testing.T, id, bookingNo string) zinc.BookingKtmbCostMissingRes {
	t.Helper()
	var parsed openapi_types.UUID
	if err := parsed.UnmarshalText([]byte(id)); err != nil {
		t.Fatal(err)
	}
	return zinc.BookingKtmbCostMissingRes{
		Id:          parsed,
		BookingNo:   bookingNo,
		TicketNo:    "T-" + bookingNo,
		CompletedAt: "2026-07-01T00:00:00Z",
	}
}

func ticketResult(bookingNo string, amount float32, currency string) ktmb.GenericRes[ktmb.GetTicketRes] {
	return ktmb.GenericRes[ktmb.GetTicketRes]{
		Status: true,
		Data: ktmb.GetTicketRes{Bookings: []ktmb.GetTicketBookingRes{{
			BookingNo:    bookingNo,
			TotalAmount:  amount,
			CurrencyCode: currency,
		}}},
	}
}

func invalidSessionResult() ktmb.GenericRes[ktmb.GetTicketRes] {
	return ktmb.GenericRes[ktmb.GetTicketRes]{
		Status:   false,
		Messages: []string{"Please login to continue."},
	}
}

func rawTicketResult(t *testing.T, bookingNo string, refundAmount *float32) ktmb.GenericRes[json.RawMessage] {
	t.Helper()
	ticket := map[string]any{
		"ticketNo":   "T-" + bookingNo,
		"ticketData": "ticket-data-" + bookingNo,
	}
	if refundAmount != nil {
		ticket["refundAmount"] = *refundAmount
		ticket["refundCurrency"] = "myr"
	}
	data, err := json.Marshal(map[string]any{"bookings": []any{map[string]any{
		"bookingNo":    bookingNo,
		"bookingData":  "booking-data-" + bookingNo,
		"currencyCode": "MYR",
		"trips":        []any{map[string]any{"tickets": []any{ticket}}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return ktmb.GenericRes[json.RawMessage]{Status: true, Data: data}
}

func testClient(t *testing.T, stub *zincStub, tickets *fakeTickets, session *fakeSession, options Options) (*Client, func()) {
	t.Helper()
	server := stub.server()
	zincClient, err := zinc.NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.Nop()
	client := New(zincClient, tickets, session, &logger, "email", "password", options)
	client.sleep = func(context.Context, time.Duration) error { return nil }
	return client, server.Close
}

func TestRunPagesDedupesAndPostsExactCost(t *testing.T) {
	items := []zinc.BookingKtmbCostMissingRes{
		workItem(t, idA, "B-A"),
		workItem(t, idB, "B-B"),
		workItem(t, idC, "B-C"),
	}
	stub := &zincStub{t: t, pages: map[int][]zinc.BookingKtmbCostMissingRes{
		0: {items[0], items[1]},
		2: {items[1], items[2]},
		4: {},
	}}
	tickets := &fakeTickets{results: map[string]ktmb.GenericRes[ktmb.GetTicketRes]{
		"B-A": ticketResult("B-A", 15.5, "myr"),
		"B-B": ticketResult("B-B", 22, ""),
		"B-C": ticketResult("B-C", 30.25, "MYR"),
	}}
	client, closeServer := testClient(t, stub, tickets, &fakeSession{}, Options{Max: 10, PageSize: 2})
	defer closeServer()

	summary, err := client.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := stub.listSkips, []int{0, 2, 4}; !equalInts(got, want) {
		t.Fatalf("list skips = %v, want %v", got, want)
	}
	if len(stub.posts) != 3 {
		t.Fatalf("posts = %+v, want exactly 3 unique bookings", stub.posts)
	}
	if stub.posts[0].body.Amount != 15.5 || stub.posts[0].body.Currency != "MYR" {
		t.Errorf("first cost = %+v, want 15.5 MYR", stub.posts[0].body)
	}
	if stub.posts[1].body.Currency != "MYR" {
		t.Errorf("blank KTMB currency should default to MYR, got %+v", stub.posts[1].body)
	}
	if summary.Fetched != 3 || summary.Attempted != 3 || summary.Updated != 3 || summary.Failed != 0 {
		t.Errorf("unexpected summary: %+v", summary)
	}
}

func TestRunSecondPassSkipsAlreadyUpdatedWork(t *testing.T) {
	item := workItem(t, idA, "B-A")
	stub := &zincStub{t: t, dynamic: true, missing: []zinc.BookingKtmbCostMissingRes{item}}
	tickets := &fakeTickets{results: map[string]ktmb.GenericRes[ktmb.GetTicketRes]{
		"B-A": ticketResult("B-A", 10, "MYR"),
	}}
	session := &fakeSession{}
	client, closeServer := testClient(t, stub, tickets, session, Options{Max: 10, PageSize: 2})
	defer closeServer()

	if _, err := client.Run(context.Background()); err != nil {
		t.Fatalf("first Run returned error: %v", err)
	}
	second, err := client.Run(context.Background())
	if err != nil {
		t.Fatalf("second Run returned error: %v", err)
	}
	if len(stub.posts) != 1 || len(tickets.calls) != 1 {
		t.Fatalf("rerun should not reprocess updated work: posts=%d KTMB calls=%d", len(stub.posts), len(tickets.calls))
	}
	if second.Fetched != 0 || second.Attempted != 0 || session.calls != 1 {
		t.Errorf("empty rerun should stop before login: summary=%+v login calls=%d", second, session.calls)
	}
}

func TestRunContinuesPastPerItemFailures(t *testing.T) {
	items := []zinc.BookingKtmbCostMissingRes{
		workItem(t, idA, "B-A"),
		workItem(t, idB, "B-B"),
		workItem(t, idC, "B-C"),
		workItem(t, idD, "B-D"),
	}
	stub := &zincStub{
		t:            t,
		pages:        map[int][]zinc.BookingKtmbCostMissingRes{0: items},
		postStatuses: map[string]int{idC: http.StatusInternalServerError},
	}
	tickets := &fakeTickets{
		results: map[string]ktmb.GenericRes[ktmb.GetTicketRes]{
			"B-A": ticketResult("B-A", 10, "MYR"),
			"B-C": ticketResult("B-C", 30, "MYR"),
			"B-D": ticketResult("B-D", 40, "MYR"),
		},
		errors: map[string]error{"B-B": errors.New("KTMB unavailable")},
	}
	client, closeServer := testClient(t, stub, tickets, &fakeSession{}, Options{Max: 10, PageSize: 10})
	defer closeServer()

	summary, err := client.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(tickets.calls) != 4 {
		t.Errorf("KTMB calls = %v, want all four attempted", tickets.calls)
	}
	if len(stub.posts) != 3 || stub.posts[2].id != idD {
		t.Errorf("zinc posts = %+v, want A, C, and D despite failures", stub.posts)
	}
	if summary.Attempted != 4 || summary.Updated != 2 || summary.Failed != 2 {
		t.Errorf("unexpected summary: %+v", summary)
	}
}

func TestRunDryRunFetchesCostsWithoutPosting(t *testing.T) {
	items := []zinc.BookingKtmbCostMissingRes{
		workItem(t, idA, "B-A"),
		workItem(t, idB, "B-B"),
	}
	stub := &zincStub{t: t, pages: map[int][]zinc.BookingKtmbCostMissingRes{0: items, 2: {}}}
	tickets := &fakeTickets{results: map[string]ktmb.GenericRes[ktmb.GetTicketRes]{
		"B-A": ticketResult("B-A", 10, "MYR"),
		"B-B": ticketResult("B-B", 20, "MYR"),
	}}
	client, closeServer := testClient(t, stub, tickets, &fakeSession{}, Options{DryRun: true, Max: 10, PageSize: 2})
	defer closeServer()

	summary, err := client.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(tickets.calls) != 2 || len(stub.posts) != 0 {
		t.Fatalf("dry run should resolve both KTMB costs and post none: KTMB=%v posts=%v", tickets.calls, stub.posts)
	}
	if summary.DryRun != 2 || summary.Updated != 0 || summary.Failed != 0 {
		t.Errorf("unexpected summary: %+v", summary)
	}
}

func TestRunReloginsAndRetriesInvalidSessionOnce(t *testing.T) {
	item := workItem(t, idA, "B-A")
	stub := &zincStub{t: t, pages: map[int][]zinc.BookingKtmbCostMissingRes{0: {item}}}
	tickets := &fakeTickets{sequences: map[string][]ktmb.GenericRes[ktmb.GetTicketRes]{
		"B-A": {invalidSessionResult(), ticketResult("B-A", 15, "MYR")},
	}}
	session := &fakeSession{tokens: []string{"stale-user-data", "fresh-user-data"}}
	client, closeServer := testClient(t, stub, tickets, session, Options{Max: 10, PageSize: 10})
	defer closeServer()

	summary, err := client.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := tickets.userData, []string{"stale-user-data", "fresh-user-data"}; !equalStrings(got, want) {
		t.Fatalf("GetTicket userData = %v, want %v", got, want)
	}
	if session.calls != 2 || !equalStrings(session.invalidations, []string{"stale-user-data"}) {
		t.Fatalf("session calls = %d, invalidations = %v; want 2 logins and stale token invalidated", session.calls, session.invalidations)
	}
	if summary.Attempted != 1 || summary.Updated != 1 || summary.Failed != 0 {
		t.Errorf("unexpected summary: %+v", summary)
	}
}

func TestRunCountsFailureWhenRetriedSessionIsStillInvalid(t *testing.T) {
	items := []zinc.BookingKtmbCostMissingRes{
		workItem(t, idA, "B-A"),
		workItem(t, idB, "B-B"),
	}
	stub := &zincStub{t: t, pages: map[int][]zinc.BookingKtmbCostMissingRes{0: items}}
	tickets := &fakeTickets{
		results: map[string]ktmb.GenericRes[ktmb.GetTicketRes]{
			"B-B": ticketResult("B-B", 20, "MYR"),
		},
		sequences: map[string][]ktmb.GenericRes[ktmb.GetTicketRes]{
			"B-A": {invalidSessionResult(), invalidSessionResult()},
		},
	}
	session := &fakeSession{tokens: []string{"stale-user-data", "fresh-user-data"}}
	client, closeServer := testClient(t, stub, tickets, session, Options{Max: 10, PageSize: 10})
	defer closeServer()

	summary, err := client.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got, want := tickets.calls, []string{"B-A", "B-A", "B-B"}; !equalStrings(got, want) {
		t.Fatalf("GetTicket calls = %v, want exactly one retry then continue with %v", got, want)
	}
	if session.calls != 2 || len(session.invalidations) != 1 {
		t.Fatalf("session calls = %d, invalidations = %v; want one refresh only", session.calls, session.invalidations)
	}
	if summary.Attempted != 2 || summary.Updated != 1 || summary.Failed != 1 {
		t.Errorf("unexpected summary: %+v", summary)
	}
}

func TestRunPassesTerminatedStatusAndRejectsMissingPurchaseAmount(t *testing.T) {
	item := workItem(t, idA, "B-A")
	stub := &zincStub{t: t, pages: map[int][]zinc.BookingKtmbCostMissingRes{0: {item}}}
	tickets := &fakeTickets{results: map[string]ktmb.GenericRes[ktmb.GetTicketRes]{
		"B-A": ticketResult("B-A", 0, "MYR"),
	}}
	client, closeServer := testClient(t, stub, tickets, &fakeSession{}, Options{Status: StatusTerminated, Max: 10, PageSize: 10})
	defer closeServer()

	summary, err := client.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !equalInts(stub.listStatuses, []int{StatusTerminated}) {
		t.Fatalf("Status queries = %v, want terminated (%d)", stub.listStatuses, StatusTerminated)
	}
	if summary.Failed != 1 || summary.Updated != 0 || len(stub.posts) != 0 {
		t.Fatalf("missing cancelled-ticket amount must fail without posting: summary=%+v posts=%v", summary, stub.posts)
	}
}

func TestRunRefundBackfillUsesExactPolicyAmount(t *testing.T) {
	item := workItem(t, idA, "B-A")
	stub := &zincStub{t: t, missing: []zinc.BookingKtmbCostMissingRes{item}}
	tickets := &fakeTickets{
		rawResults: map[string]ktmb.GenericRes[json.RawMessage]{"B-A": rawTicketResult(t, "B-A", nil)},
		policies: map[string]ktmb.GenericRes[ktmb.RefundPolicyRes]{
			"booking-data-B-A": {
				Status: true,
				Data: ktmb.RefundPolicyRes{
					CurrencyCode: "MYR",
					Trips: []ktmb.RefundPolicyTripRes{{
						Tickets: []ktmb.RefundPolicyTripTicketRes{{
							TicketNo: "T-B-A", RefundAmount: 7.5, CurrencyCode: "myr",
						}},
					}},
				},
			},
		},
	}
	client, closeServer := testClient(t, stub, tickets, &fakeSession{}, Options{Refund: true, Status: StatusTerminated, Max: 10, PageSize: 10})
	defer closeServer()

	summary, err := client.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(stub.refunds) != 1 || stub.refunds[0].body.RefundAmount != 7.5 || stub.refunds[0].body.RefundCurrency != "MYR" {
		t.Fatalf("refund posts = %+v, want exact 7.5 MYR", stub.refunds)
	}
	if !equalStrings(tickets.policyCalls, []string{"booking-data-B-A"}) || summary.Updated != 1 || summary.Failed != 0 {
		t.Fatalf("policy calls=%v summary=%+v", tickets.policyCalls, summary)
	}
}

func TestRunRefundDryRunFindsRawAmountAndDoesNotPost(t *testing.T) {
	item := workItem(t, idA, "B-A")
	amount := float32(6.25)
	stub := &zincStub{t: t, missing: []zinc.BookingKtmbCostMissingRes{item}}
	tickets := &fakeTickets{
		rawResults: map[string]ktmb.GenericRes[json.RawMessage]{"B-A": rawTicketResult(t, "B-A", &amount)},
		policies: map[string]ktmb.GenericRes[ktmb.RefundPolicyRes]{
			"booking-data-B-A": {Status: false, Messages: []string{"Refund is no longer allowed"}},
		},
	}
	client, closeServer := testClient(t, stub, tickets, &fakeSession{}, Options{DryRun: true, Refund: true, Status: StatusTerminated, Max: 10, PageSize: 10})
	defer closeServer()

	summary, err := client.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(stub.refunds) != 0 || summary.DryRun != 1 || summary.Failed != 0 {
		t.Fatalf("dry-run refund summary=%+v posts=%+v", summary, stub.refunds)
	}
}

func TestRunRefundLeavesMissingWhenKTMBHasNoExactAmount(t *testing.T) {
	item := workItem(t, idA, "B-A")
	stub := &zincStub{t: t, missing: []zinc.BookingKtmbCostMissingRes{item}}
	tickets := &fakeTickets{
		rawResults: map[string]ktmb.GenericRes[json.RawMessage]{"B-A": rawTicketResult(t, "B-A", nil)},
		policies: map[string]ktmb.GenericRes[ktmb.RefundPolicyRes]{
			"booking-data-B-A": {Status: false, Messages: []string{"Refund is no longer allowed"}},
		},
	}
	client, closeServer := testClient(t, stub, tickets, &fakeSession{}, Options{Refund: true, Status: StatusTerminated, Max: 10, PageSize: 10})
	defer closeServer()

	summary, err := client.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(stub.refunds) != 0 || summary.Failed != 1 || summary.Updated != 0 {
		t.Fatalf("unknown refund must remain missing: summary=%+v posts=%+v", summary, stub.refunds)
	}
}

func TestRefundFromPolicyDoesNotAssignAMultiTicketTotalToOneTicket(t *testing.T) {
	policy := ktmb.RefundPolicyRes{
		TotalRefundAmount: 12,
		CurrencyCode:      "MYR",
		Trips: []ktmb.RefundPolicyTripRes{{Tickets: []ktmb.RefundPolicyTripTicketRes{
			{TicketNo: "T-1"},
			{TicketNo: "T-2", RefundAmount: 12},
		}}},
	}

	if amount, currency, ok := refundFromPolicy(policy, "T-1"); ok {
		t.Fatalf("refundFromPolicy = (%v, %q, true), want no exact amount for T-1", amount, currency)
	}
}

func TestRefundFromRawDoesNotUseAnotherTicketsAmount(t *testing.T) {
	raw := json.RawMessage(`{
		"bookings": [{
			"bookingNo": "B-1",
			"trips": [{"tickets": [
				{"ticketNo": "T-1"},
				{"ticketNo": "T-2", "refundAmount": 7.5, "refundCurrency": "MYR"}
			]}]
		}]
	}`)

	if amount, currency, ok := refundFromRaw(raw, "T-1"); ok {
		t.Fatalf("refundFromRaw = (%v, %q, true), want no exact amount for T-1", amount, currency)
	}
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
