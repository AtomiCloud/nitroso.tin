package zinc

// This file hand-extends the generated client for zinc's KTMB actual-cost
// endpoints pending swagger regeneration.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	openapi_types "github.com/deepmap/oapi-codegen/pkg/types"
)

type BookingKtmbCostMissingRes struct {
	Id          openapi_types.UUID `json:"id"`
	BookingNo   string             `json:"bookingNo"`
	TicketNo    string             `json:"ticketNo"`
	CompletedAt string             `json:"completedAt"`
}

type GetApiVVersionBookingKtmbCostMissingParams struct {
	Limit *int32 `form:"Limit,omitempty" json:"Limit,omitempty"`
	Skip  *int32 `form:"Skip,omitempty" json:"Skip,omitempty"`
}

type PostApiVVersionBookingIdKtmbCostJSONBody struct {
	Amount   float32 `json:"amount"`
	Currency string  `json:"currency"`
}

func (c *Client) GetApiVVersionBookingKtmbCostMissing(ctx context.Context, version string, params *GetApiVVersionBookingKtmbCostMissingParams, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewGetApiVVersionBookingKtmbCostMissingRequest(c.Server, version, params)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func (c *Client) PostApiVVersionBookingIdKtmbCost(ctx context.Context, version string, id openapi_types.UUID, body PostApiVVersionBookingIdKtmbCostJSONBody, reqEditors ...RequestEditorFn) (*http.Response, error) {
	req, err := NewPostApiVVersionBookingIdKtmbCostRequest(c.Server, version, id, body)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	if err := c.applyEditors(ctx, req, reqEditors); err != nil {
		return nil, err
	}
	return c.Client.Do(req)
}

func NewGetApiVVersionBookingKtmbCostMissingRequest(server, version string, params *GetApiVVersionBookingKtmbCostMissingParams) (*http.Request, error) {
	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}
	queryURL, err := serverURL.Parse(fmt.Sprintf("./api/v%s/Booking/ktmb-cost/missing", url.PathEscape(version)))
	if err != nil {
		return nil, err
	}
	if params != nil {
		values := queryURL.Query()
		if params.Limit != nil {
			values.Set("Limit", fmt.Sprint(*params.Limit))
		}
		if params.Skip != nil {
			values.Set("Skip", fmt.Sprint(*params.Skip))
		}
		queryURL.RawQuery = values.Encode()
	}
	return http.NewRequest(http.MethodGet, queryURL.String(), nil)
}

func NewPostApiVVersionBookingIdKtmbCostRequest(server, version string, id openapi_types.UUID, body PostApiVVersionBookingIdKtmbCostJSONBody) (*http.Request, error) {
	serverURL, err := url.Parse(server)
	if err != nil {
		return nil, err
	}
	requestURL, err := serverURL.Parse(fmt.Sprintf("./api/v%s/Booking/%s/ktmb-cost", url.PathEscape(version), url.PathEscape(id.String())))
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, requestURL.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}
