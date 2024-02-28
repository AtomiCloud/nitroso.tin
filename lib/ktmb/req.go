package ktmb

// Login
type LoginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Search
type SearchReq struct {
	FromStationData string `json:"FromStationData"`
	FromStationID   string `json:"FromStationId"`
	OnwardDate      string `json:"OnwardDate"`
	PassengerCount  int64  `json:"PassengerCount"`
	ToStationData   string `json:"ToStationData"`
	ToStationID     string `json:"ToStationId"`
}

// Trip Req
type TripReq struct {
	BookingTripSequenceNo int64  `json:"BookingTripSequenceNo"`
	DepartDate            string `json:"DepartDate"`
	SearchData            string `json:"searchData"`
}

// Reserve
type ReserveReq struct {
	AppInformation string `json:"appInformation"`
	SearchData     string `json:"searchData"`
	Trips          []Trip `json:"trips"`
}

type Trip struct {
	TripData string `json:"tripData"`
}

// BookStart
type BookStartReq struct {
	BookingData string `json:"bookingData"`
}

// Set Passenger
type SetPassengerReq struct {
	BookingData string         `json:"bookingData"`
	Passengers  []PassengerReq `json:"passengers"`
}

type PassengerReq struct {
	ContactNo          string      `json:"contactNo"`
	FullName           string      `json:"fullName"`
	Gender             string      `json:"gender"`
	IsAddFavorite      bool        `json:"isAddFavorite"`
	IsBuyInsurance     bool        `json:"isBuyInsurance"`
	IsSelf             bool        `json:"isSelf"`
	PassengerData      string      `json:"passengerData"`
	PassportExpiryDate string      `json:"passportExpiryDate"`
	PassportNo         string      `json:"passportNo"`
	PnrData            string      `json:"pnrData"`
	PnrNo              string      `json:"pnrNo"`
	TicketTypeID       string      `json:"ticketTypeId"`
	Tickets            []TicketReq `json:"tickets"`
}

type TicketReq struct {
	PromoCode string `json:"promoCode"`
}

// Pay
type PayReq struct {
	BookingData    string  `json:"bookingData"`
	DiscountAmount float32 `json:"discountAmount"`
	EWalletAmount  float32 `json:"eWalletAmount"`
	PaymentAmount  float32 `json:"paymentAmount"`
	TotalAmount    float32 `json:"totalAmount"`
	PaymentMethod  string  `json:"paymentMethod"`
}

// Complete Req
type CompleteReq struct {
	BookingData string `json:"bookingData"`
}

// Print Tix
type PrintTicketReq struct {
	BookingNo string                 `json:"bookingNo"`
	Tickets   []PrintTicketTicketReq `json:"tickets"`
}

type PrintTicketTicketReq struct {
	TicketNo string `json:"ticketNo"`
}

// CancelReserve
type CancelReserveReq struct {
	BookingData string `json:"bookingData"`
}
