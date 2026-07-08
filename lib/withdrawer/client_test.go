package withdrawer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/AtomiCloud/nitroso-tin/lib/zinc"
	"github.com/AtomiCloud/nitroso-tin/system/config"
	openapi_types "github.com/deepmap/oapi-codegen/pkg/types"
	"github.com/rs/zerolog"
)

const (
	idA = "aaaaaaaa-0000-4000-8000-000000000001"
	idB = "aaaaaaaa-0000-4000-8000-000000000002"
	idC = "aaaaaaaa-0000-4000-8000-000000000003"
)

// wd builds a minimal withdrawal principal with the given id.
func wd(t *testing.T, id string) zinc.WithdrawalPrincipalRes {
	t.Helper()
	var u openapi_types.UUID
	if err := u.UnmarshalText([]byte(id)); err != nil {
		t.Fatal(err)
	}
	return zinc.WithdrawalPrincipalRes{Id: u}
}

// approveResult overrides the stub's response to one withdrawal's approve
// call; the zero value (unset map entry) means 200 OK.
type approveResult struct {
	status int
	body   string
}

// zincStub fakes the two zinc endpoints the withdrawer touches: the paged
// Status-filtered listing and the approve endpoint. It serves pre-baked pages
// per status (page index = Skip/Limit, beyond the last page is empty) and
// records every approve call, so tests control paging shape — including
// duplicates from a shrinking set — and per-item approve outcomes exactly.
type zincStub struct {
	t         *testing.T
	pages     map[string][][]zinc.WithdrawalPrincipalRes // status -> successive pages
	approve   map[string]approveResult                   // withdrawal id -> forced response
	approved  []string                                   // approve calls, in order
	listCalls map[string]int                             // status -> number of list calls
}

func (s *zincStub) server() *httptest.Server {
	s.listCalls = map[string]int{}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1.0/Withdrawal":
			status := r.URL.Query().Get("Status")
			limit, err := strconv.Atoi(r.URL.Query().Get("Limit"))
			if err != nil || limit <= 0 {
				s.t.Errorf("list called with bad Limit %q", r.URL.Query().Get("Limit"))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			skip, err := strconv.Atoi(r.URL.Query().Get("Skip"))
			if err != nil {
				s.t.Errorf("list called with bad Skip %q", r.URL.Query().Get("Skip"))
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			s.listCalls[status]++
			page := []zinc.WithdrawalPrincipalRes{}
			if idx := skip / limit; idx < len(s.pages[status]) {
				page = s.pages[status][idx]
			}
			if err := json.NewEncoder(w).Encode(page); err != nil {
				s.t.Fatal(err)
			}
		case r.Method == http.MethodPost &&
			strings.HasPrefix(r.URL.Path, "/api/v1.0/Withdrawal/") &&
			strings.HasSuffix(r.URL.Path, "/approve"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1.0/Withdrawal/"), "/approve")
			s.approved = append(s.approved, id)
			if res, ok := s.approve[id]; ok {
				w.WriteHeader(res.status)
				_, _ = w.Write([]byte(res.body))
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			s.t.Errorf("unexpected zinc call: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

func sweepClient(t *testing.T, stub *zincStub, limit int) (*Client, func()) {
	t.Helper()
	srv := stub.server()
	zc, err := zinc.NewClient(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	l := zerolog.Nop()
	return &Client{zinc: zc, logger: &l, config: config.WithdrawerConfig{Cron: "0 0 0 * * *", Limit: limit}}, srv.Close
}

// The listing assembles all pages of a status and stops paging on the first
// short page, and Processing withdrawals are swept alongside Pending ones.
func TestSweepPagingAssemblesPagesAndStopsOnShortPage(t *testing.T) {
	stub := &zincStub{t: t, pages: map[string][][]zinc.WithdrawalPrincipalRes{
		// full page then a short page: paging must stop after page 2
		"Pending": {
			{wd(t, idA), wd(t, idB)},
			{wd(t, idC)},
		},
	}}
	c, closeSrv := sweepClient(t, stub, 2)
	defer closeSrv()

	summary, err := c.sweep(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	want := []string{idA, idB, idC}
	if len(stub.approved) != len(want) {
		t.Fatalf("expected approvals %v, got %v", want, stub.approved)
	}
	for i, id := range want {
		if stub.approved[i] != id {
			t.Errorf("approval %d: expected %s, got %s", i, id, stub.approved[i])
		}
	}
	if stub.listCalls["Pending"] != 2 {
		t.Errorf("expected paging to stop on the short page after 2 Pending list calls, got %d", stub.listCalls["Pending"])
	}
	if stub.listCalls["Processing"] != 1 {
		t.Errorf("expected exactly 1 Processing list call, got %d", stub.listCalls["Processing"])
	}
	if summary.total != 3 || summary.succeeded != 3 || summary.skipped != 0 || summary.failed != 0 {
		t.Errorf("unexpected summary %+v", summary)
	}
}

// Skip/Limit paging over a shrinking set can return the same id twice —
// within a status' pages or across the Pending and Processing listings — and
// each id must still be approved exactly once.
func TestSweepDedupesDuplicateIdsAcrossPages(t *testing.T) {
	stub := &zincStub{t: t, pages: map[string][][]zinc.WithdrawalPrincipalRes{
		// B repeats on the second Pending page (set shrank mid-listing) and
		// A repeats in the Processing listing
		"Pending": {
			{wd(t, idA), wd(t, idB)},
			{wd(t, idB), wd(t, idC)},
		},
		"Processing": {
			{wd(t, idA)},
		},
	}}
	c, closeSrv := sweepClient(t, stub, 2)
	defer closeSrv()

	summary, err := c.sweep(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	want := []string{idA, idB, idC}
	if len(stub.approved) != len(want) {
		t.Fatalf("expected each id approved exactly once %v, got %v", want, stub.approved)
	}
	for i, id := range want {
		if stub.approved[i] != id {
			t.Errorf("approval %d: expected %s, got %s", i, id, stub.approved[i])
		}
	}
	if summary.total != 3 || summary.succeeded != 3 {
		t.Errorf("unexpected summary %+v", summary)
	}
}

// One failing approve must not abort the sweep: the remaining withdrawals are
// still attempted and the tallies attribute exactly one failure.
func TestSweepContinuesPastFailureAndTallies(t *testing.T) {
	stub := &zincStub{
		t: t,
		pages: map[string][][]zinc.WithdrawalPrincipalRes{
			"Pending": {{wd(t, idA), wd(t, idB), wd(t, idC)}},
		},
		approve: map[string]approveResult{
			idB: {status: http.StatusInternalServerError, body: "boom"},
		},
	}
	c, closeSrv := sweepClient(t, stub, 10)
	defer closeSrv()

	summary, err := c.sweep(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(stub.approved) != 3 {
		t.Errorf("expected all 3 approves attempted despite the failure, got %v", stub.approved)
	}
	if summary.total != 3 || summary.succeeded != 2 || summary.failed != 1 || summary.skipped != 0 {
		t.Errorf("unexpected summary %+v", summary)
	}
}

// zinc rejecting an approve with InvalidWithdrawalOperation (e.g. a
// Processing withdrawal whose payout is already confirmed) is benign: it must
// be tallied as skipped, never as failed. Both problem-details shapes are
// recognised — the type URL (error portal enabled) and the bare title
// (portal disabled).
func TestSweepSkipsBenignAlreadyConfirmedRejection(t *testing.T) {
	stub := &zincStub{
		t: t,
		pages: map[string][][]zinc.WithdrawalPrincipalRes{
			"Processing": {{wd(t, idA), wd(t, idB), wd(t, idC)}},
		},
		approve: map[string]approveResult{
			// confirmed: rejected via the problem type URL
			idA: {
				status: http.StatusBadRequest,
				body:   `{"type":"https://api.zinc.bunnybooker.com/docs/pichu/nitroso/zinc/main/v1/invalid_withdrawal_operation","title":"Invalid Withdrawal Operation","status":400}`,
			},
			// confirmed: rejected with the title only (error portal disabled)
			idC: {
				status: http.StatusConflict,
				body:   `{"title":"Invalid Withdrawal Operation","status":409}`,
			},
		},
	}
	c, closeSrv := sweepClient(t, stub, 10)
	defer closeSrv()

	summary, err := c.sweep(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if summary.total != 3 || summary.succeeded != 1 || summary.skipped != 2 || summary.failed != 0 {
		t.Errorf("unexpected summary %+v", summary)
	}
}

// A 4xx that is NOT InvalidWithdrawalOperation stays a real failure — the
// benign-skip carve-out must not swallow other client errors.
func TestSweepOther4xxStillCountsAsFailed(t *testing.T) {
	stub := &zincStub{
		t: t,
		pages: map[string][][]zinc.WithdrawalPrincipalRes{
			"Pending": {{wd(t, idA)}},
		},
		approve: map[string]approveResult{
			idA: {status: http.StatusBadRequest, body: `{"title":"Validation Error","status":400}`},
		},
	}
	c, closeSrv := sweepClient(t, stub, 10)
	defer closeSrv()

	summary, err := c.sweep(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if summary.total != 1 || summary.failed != 1 || summary.skipped != 0 || summary.succeeded != 0 {
		t.Errorf("unexpected summary %+v", summary)
	}
}
