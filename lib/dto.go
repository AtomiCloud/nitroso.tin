package lib

// RecoverDto is a recovery work item for a booking whose KTMB purchase hit a
// conflict (duplicate passport) or whose captured ticket could not be reported
// back to zinc. Pushed by the buyer onto the recover queue, drained by the
// recoverer. BookingNo/TicketNo are set only when the KTMB purchase is known
// to have succeeded — the recoverer can then force-complete without scanning.
type RecoverDto struct {
	BookingId      string `json:"bookingId"`
	Direction      string `json:"direction"`
	Date           string `json:"date"`
	Time           string `json:"time"`
	FullName       string `json:"fullName"`
	Gender         string `json:"gender"`
	PassportExpiry string `json:"passportExpiry"`
	PassportNumber string `json:"passportNumber"`
	BookingNo      string `json:"bookingNo"`
	TicketNo       string `json:"ticketNo"`
	Attempts       int    `json:"attempts"`
}
