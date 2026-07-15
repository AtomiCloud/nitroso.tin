package terminator

import openapi_types "github.com/deepmap/oapi-codegen/pkg/types"

type BookingTermination struct {
	Id        openapi_types.UUID `json:"id"`
	BookingNo string             `json:"bookingNo"`
	TicketNo  string             `json:"ticketNo"`
}
