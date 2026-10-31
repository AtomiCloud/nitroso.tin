package zinc

// This file hand-extends the generated client for zinc's recovery-window and
// KTMB account-check endpoints pending swagger regeneration.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	openapi_types "github.com/deepmap/oapi-codegen/pkg/types"
)

// PostApiVVersionBookingKtmbCheckCheckIdResultJSONBody is tin's answer to an
// admin "check our KTMB account" request. Status is one of Found, NotFound,
// Departed, Error.
type PostApiVVersionBookingKtmbCheckCheckIdResultJSONBody struct {
	Status             string              `json:"status"`
	KtmbBookingNo      *string             `json:"ktmbBookingNo"`
	KtmbTicketNo       *string             `json:"ktmbTicketNo"`
	ClaimedByBookingId *openapi_types.UUID `json:"claimedByBookingId"`
	Error              *string             `json:"error"`
}

// PostApiVVersionBookingRecoveryExpire ends recycled bookings still in
// Pending past their recovery window / departure lead as Duplicate; answers
// the list of bookings it ended.
func (c *Client) PostApiVVersionBookingRecoveryExpire(ctx context.Context, version string, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewPostApiVVersionBookingRecoveryExpireRequest(c.Server, version)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) PostApiVVersionBookingKtmbCheckCheckIdResult(ctx context.Context, version string, checkId openapi_types.UUID, body PostApiVVersionBookingKtmbCheckCheckIdResultJSONBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewPostApiVVersionBookingKtmbCheckCheckIdResultRequest(c.Server, version, checkId, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func NewPostApiVVersionBookingRecoveryExpireRequest(server, version string) (*http.Request, error) {
	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}
	requestURL, err := serverURL.Parse(fmt.Sprintf("./api/v%s/Booking/recovery-expire", url.PathEscape(version)))
	if err != nil {
		return nil, err
	}
	return http.NewRequest(http.MethodPost, requestURL.String(), nil)
}

func NewPostApiVVersionBookingKtmbCheckCheckIdResultRequest(server, version string, checkId openapi_types.UUID, body PostApiVVersionBookingKtmbCheckCheckIdResultJSONBody) (*http.Request, error) {
	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}
	requestURL, err := serverURL.Parse(fmt.Sprintf("./api/v%s/Booking/ktmb-check/%s/result", url.PathEscape(version), url.PathEscape(checkId.String())))
	if err != nil {
		return nil, err
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, requestURL.String(), bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	return req, nil
}
