package ktmb

type GenericRes[T any] struct {
	Status      bool        `json:"status"`
	Messages    []string    `json:"messages"`
	MessageCode interface{} `json:"messageCode"`
	Data        T           `json:"data"`
}

// Login
type LoginRes struct {
	Email           string  `json:"email"`
	EWalletCurrency string  `json:"eWalletCurrency"`
	EWalletAmount   float64 `json:"eWalletAmount"`
	FullName        string  `json:"fullName"`
	PnrNo           string  `json:"pnrNo"`
	UserData        string  `json:"userData"`
}

// Station
type StationsAllRes struct {
	MaximumPassengerCount int64        `json:"maximumPassengerCount"`
	Today                 string       `json:"today"`
	Stations              []StationRes `json:"stations"`
}

type StationRes struct {
	ID            string   `json:"id"`
	Description   string   `json:"description"`
	State         string   `json:"state"`
	Region        string   `json:"region"`
	Country       string   `json:"country"`
	Currency      string   `json:"currency"`
	StationData   string   `json:"stationData"`
	TrainServices []string `json:"trainServices"`
}

// Search
type SearchStationsRes struct {
	BookingTripCount int64  `json:"bookingTripCount"`
	SearchData       string `json:"searchData"`
}

// Trip
type TripAllRes struct {
	FromStationDescription string    `json:"fromStationDescription"`
	ToStationDescription   string    `json:"toStationDescription"`
	DepartDate             string    `json:"departDate"`
	IsReturn               bool      `json:"isReturn"`
	TrainService           string    `json:"trainService"`
	Trips                  []TripRes `json:"trips"`
}

type TripRes struct {
	ID               string  `json:"id"`
	TrainNo          string  `json:"trainNo"`
	ServiceCategory  string  `json:"serviceCategory"`
	DepartDateTime   string  `json:"departDateTime"`
	ArrivalDateTime  string  `json:"arrivalDateTime"`
	ArrivalDayOffset int64   `json:"arrivalDayOffset"`
	Currency         string  `json:"currency"`
	Price            float32 `json:"price"`
	Seat             int64   `json:"seat"`
	TripData         string  `json:"tripData"`
}

// Reserve
type ReserveRes struct {
	BookingData            string `json:"bookingData"`
	IsBookingTripCompleted bool   `json:"isBookingTripCompleted"`
}

// BookStart
type BookStartRes struct {
	BookingData               string             `json:"bookingData"`
	UserData                  string             `json:"userData"`
	BookingExpiryDateTime     string             `json:"bookingExpiryDateTime"`
	BookingExpiryTimeSpan     map[string]float64 `json:"bookingExpiryTimeSpan"`
	PassengerCount            int64              `json:"passengerCount"`
	HasReturn                 bool               `json:"hasReturn"`
	CurrencyCode              string             `json:"currencyCode"`
	TotalTicketAmount         float32            `json:"totalTicketAmount"`
	TotalAddOnAmount          float32            `json:"totalAddOnAmount"`
	RoundingAmount            float32            `json:"roundingAmount"`
	TotalAmount               float32            `json:"totalAmount"`
	PassportMinimumExpiryDate string             `json:"passportMinimumExpiryDate"`
	EWalletBalanceAmount      float32            `json:"eWalletBalanceAmount"`
	Self                      SelfRes            `json:"self"`
	Favorites                 []SelfRes          `json:"favorites"`
	Passengers                []PassengerRes     `json:"passengers"`
}

type SelfRes struct {
	ContactNo          string  `json:"contactNo"`
	FullName           string  `json:"fullName"`
	Gender             string  `json:"gender"`
	PassportExpiryDate *string `json:"passportExpiryDate"`
	PassportNo         string  `json:"passportNo"`
	PnrData            string  `json:"pnrData"`
	PnrNo              string  `json:"pnrNo"`
	IsConcession       *bool   `json:"isConcession,omitempty"`
}

type PassengerRes struct {
	IsAddFavorite      bool            `json:"isAddFavorite"`
	IsBuyInsurance     bool            `json:"isBuyInsurance"`
	IsSelf             bool            `json:"isSelf"`
	PassengerData      string          `json:"passengerData"`
	PassengerPNRData   interface{}     `json:"passengerPNRData"`
	PnrNo              interface{}     `json:"pnrNo"`
	BottomLabels       []string        `json:"bottomLabels"`
	ContactNo          interface{}     `json:"contactNo"`
	FullName           interface{}     `json:"fullName"`
	Gender             interface{}     `json:"gender"`
	PassportExpiryDate interface{}     `json:"passportExpiryDate"`
	PassportNo         interface{}     `json:"passportNo"`
	TicketTypeID       interface{}     `json:"ticketTypeId"`
	Tickets            []TicketRes     `json:"tickets"`
	TicketTypes        []TicketTypeRes `json:"ticketTypes"`
}

type TicketTypeRes struct {
	Description    string `json:"description"`
	ID             string `json:"id"`
	IsDefault      bool   `json:"isDefault"`
	IsEnabled      bool   `json:"isEnabled"`
	IsFreeDutyPass bool   `json:"isFreeDutyPass"`
	IsLedger       bool   `json:"isLedger"`
	IsPNR          bool   `json:"isPNR"`
	IsFreeTravel   bool   `json:"isFreeTravel"`
}

type TicketRes struct {
	TripNo            string      `json:"tripNo"`
	TripName          string      `json:"tripName"`
	FromStationName   string      `json:"fromStationName"`
	ToStationName     string      `json:"toStationName"`
	FromLocalDateTime string      `json:"fromLocalDateTime"`
	ToLocalDateTime   string      `json:"toLocalDateTime"`
	IsReturn          bool        `json:"isReturn"`
	CurrencyCode      string      `json:"currencyCode"`
	TicketAmount      float32     `json:"ticketAmount"`
	TicketData        string      `json:"ticketData"`
	TicketIndex       int64       `json:"ticketIndex"`
	TotalAddOnAmount  float32     `json:"totalAddOnAmount"`
	TotalAmount       float32     `json:"totalAmount"`
	PromoCode         interface{} `json:"promoCode"`
}

// Set Passenger
type SetPassengerRes struct {
	BookingData                     string             `json:"bookingData"`
	UserData                        string             `json:"userData"`
	BookingExpiryDateTime           string             `json:"bookingExpiryDateTime"`
	BookingExpiryTimeSpan           map[string]float64 `json:"bookingExpiryTimeSpan"`
	PassengerCount                  int64              `json:"passengerCount"`
	HasReturn                       bool               `json:"hasReturn"`
	CurrencyCode                    string             `json:"currencyCode"`
	TotalTicketAmount               float32            `json:"totalTicketAmount"`
	TotalAddOnAmount                float32            `json:"totalAddOnAmount"`
	TotalTicketInsuranceAddOnAmount float32            `json:"totalTicketInsuranceAddOnAmount"`
	RoundingAmount                  float32            `json:"roundingAmount"`
	TotalAmount                     float32            `json:"totalAmount"`
	DiscountAmount                  float32            `json:"discountAmount"`
	PaymentAmount                   float32            `json:"paymentAmount"`
	TicketCount                     int64              `json:"ticketCount"`
	TicketAddOnCount                int64              `json:"ticketAddOnCount"`
	EWalletBalanceAmount            float32            `json:"eWalletBalanceAmount"`
	PaymentMethods                  []PaymentMethodRes `json:"paymentMethods"`
	Trips                           []PaymentTripRes   `json:"trips"`
}

type PaymentMethodRes struct {
	Description      string `json:"description"`
	ID               string `json:"id"`
	ImageFileVersion string `json:"imageFileVersion"`
}

type PaymentTripRes struct {
	TrainService                    string             `json:"trainService"`
	TrainServiceLabel               interface{}        `json:"trainServiceLabel"`
	TripIndex                       int64              `json:"tripIndex"`
	TripNo                          string             `json:"tripNo"`
	TripName                        string             `json:"tripName"`
	FromStationName                 string             `json:"fromStationName"`
	ToStationName                   string             `json:"toStationName"`
	FromLocalDateTime               string             `json:"fromLocalDateTime"`
	ToLocalDateTime                 string             `json:"toLocalDateTime"`
	IsReturn                        bool               `json:"isReturn"`
	CurrencyCode                    string             `json:"currencyCode"`
	TotalTicketAmount               float32            `json:"totalTicketAmount"`
	TotalAddOnAmount                float32            `json:"totalAddOnAmount"`
	TotalInsuranceAddOnAmount       float32            `json:"totalInsuranceAddOnAmount"`
	TotalTicketInsuranceAddOnAmount float32            `json:"totalTicketInsuranceAddOnAmount"`
	TotalAmount                     float32            `json:"totalAmount"`
	TicketCount                     int64              `json:"ticketCount"`
	IsPromoCodeRequired             bool               `json:"isPromoCodeRequired"`
	Tickets                         []PaymentTicketRes `json:"tickets"`
}

type PaymentTicketRes struct {
	CoachName                   string      `json:"coachName"`
	PackageName                 interface{} `json:"packageName"`
	PassengerContactNo          string      `json:"passengerContactNo"`
	PassengerFullName           string      `json:"passengerFullName"`
	PassengerGender             string      `json:"passengerGender"`
	PassengerIdentityNo         string      `json:"passengerIdentityNo"`
	PassengerPassportExpiryDate string      `json:"passengerPassportExpiryDate"`
	PassengerPassportNo         string      `json:"passengerPassportNo"`
	PassengerPNRNo              string      `json:"passengerPNRNo"`
	SeatNo                      string      `json:"seatNo"`
	SeatTypeName                string      `json:"seatTypeName"`
	ServiceTypeName             string      `json:"serviceTypeName"`
	CurrencyCode                string      `json:"currencyCode"`
	TicketAmount                float32     `json:"ticketAmount"`
	PromoAmount                 float32     `json:"promoAmount"`
	TotalAddOnAmount            float32     `json:"totalAddOnAmount"`
	TotalAmount                 float32     `json:"totalAmount"`
	TicketTypeName              string      `json:"ticketTypeName"`
	TicketIndex                 int64       `json:"ticketIndex"`
	PassengerIndex              int64       `json:"passengerIndex"`
}

// Pay
type PaymentRes struct {
	BookingData string `json:"bookingData"`
}

// Complete
type CompleteRes struct {
	UserData                string             `json:"userData"`
	EWalletBalanceAmount    float32            `json:"eWalletBalanceAmount"`
	IsLoyaltyPointShown     bool               `json:"isLoyaltyPointShown"`
	LoyaltyPointDescription string             `json:"loyaltyPointDescription"`
	Booking                 CompleteBookingRes `json:"booking"`
}

type CompleteBookingRes struct {
	BookingData                       string             `json:"bookingData"`
	BookingLocalDateTime              string             `json:"bookingLocalDateTime"`
	BookingNo                         string             `json:"bookingNo"`
	BookingStatusLabel                string             `json:"bookingStatusLabel"`
	BookingStatusLabelBackgroundColor string             `json:"bookingStatusLabelBackgroundColor"`
	BookingStatusKey                  string             `json:"bookingStatusKey"`
	BookingTypeKey                    string             `json:"bookingTypeKey"`
	CurrencyCode                      string             `json:"currencyCode"`
	DepartFromLocalDateTime           string             `json:"departFromLocalDateTime"`
	DepartToLocalDateTime             string             `json:"departToLocalDateTime"`
	FromStationName                   string             `json:"fromStationName"`
	HasReturn                         bool               `json:"hasReturn"`
	IsPrintTicketAllowed              bool               `json:"isPrintTicketAllowed"`
	IsRefundAllowed                   bool               `json:"isRefundAllowed"`
	PassengerCount                    int64              `json:"passengerCount"`
	RoundingAmount                    float32            `json:"roundingAmount"`
	TicketAddOnCount                  int64              `json:"ticketAddOnCount"`
	TicketCount                       int64              `json:"ticketCount"`
	ToStationName                     string             `json:"toStationName"`
	TotalAddOnAmount                  float32            `json:"totalAddOnAmount"`
	TotalAmount                       float32            `json:"totalAmount"`
	TotalTicketAmount                 float32            `json:"totalTicketAmount"`
	TripCount                         int64              `json:"tripCount"`
	Payment                           CompletePaymentRes `json:"payment"`
	Trips                             []CompleteTripRes  `json:"trips"`
}

type CompletePaymentRes struct {
	BookingPaymentData     string              `json:"bookingPaymentData"`
	CompletedLocalDateTime string              `json:"completedLocalDateTime"`
	CurrencyCode           string              `json:"currencyCode"`
	DiscountAmount         float32             `json:"discountAmount"`
	PaymentAmount          float32             `json:"paymentAmount"`
	PaymentLocalDateTime   string              `json:"paymentLocalDateTime"`
	PaymentNo              string              `json:"paymentNo"`
	PaymentStatusKey       string              `json:"paymentStatusKey"`
	RoundingAmount         float32             `json:"roundingAmount"`
	TotalAddOnAmount       float32             `json:"totalAddOnAmount"`
	TotalBookingAmount     float32             `json:"totalBookingAmount"`
	TotalTicketAmount      float32             `json:"totalTicketAmount"`
	Details                []CompleteDetailRes `json:"details"`
}

type CompleteDetailRes struct {
	CurrencyCode         string  `json:"currencyCode"`
	DetailData           string  `json:"detailData"`
	PaymentAmount        float32 `json:"paymentAmount"`
	PaymentDetailTypeKey string  `json:"paymentDetailTypeKey"`
	PaymentMethodKey     string  `json:"paymentMethodKey"`
	PaymentMethodName    string  `json:"paymentMethodName"`
	UserEnteredAmount    float32 `json:"userEnteredAmount"`
	UserEnteredText      string  `json:"userEnteredText"`
}

type CompleteTripRes struct {
	CurrencyCode                    string              `json:"currencyCode"`
	FromLocalDateTime               string              `json:"fromLocalDateTime"`
	FromStationName                 string              `json:"fromStationName"`
	IsCanceled                      bool                `json:"isCanceled"`
	IsRefundAllowed                 bool                `json:"isRefundAllowed"`
	IsReturn                        bool                `json:"isReturn"`
	ToLocalDateTime                 string              `json:"toLocalDateTime"`
	ToStationName                   string              `json:"toStationName"`
	TotalAddOnAmount                float32             `json:"totalAddOnAmount"`
	TotalAmount                     float32             `json:"totalAmount"`
	TotalInsuranceAddOnAmount       float32             `json:"totalInsuranceAddOnAmount"`
	TotalTicketAmount               float32             `json:"totalTicketAmount"`
	TotalTicketInsuranceAddOnAmount float32             `json:"totalTicketInsuranceAddOnAmount"`
	TrainService                    string              `json:"trainService"`
	TrainServiceLabel               string              `json:"trainServiceLabel"`
	TrainServiceCategory            string              `json:"trainServiceCategory"`
	TripData                        string              `json:"tripData"`
	TripIndex                       int64               `json:"tripIndex"`
	TripName                        string              `json:"tripName"`
	TripNo                          string              `json:"tripNo"`
	TripStatusLabel                 string              `json:"tripStatusLabel"`
	TripStatusKey                   string              `json:"tripStatusKey"`
	Tickets                         []CompleteTicketRes `json:"tickets"`
}

type CompleteTicketRes struct {
	BoardingCode                string  `json:"boardingCode"`
	CoachName                   string  `json:"coachName"`
	CurrencyCode                string  `json:"currencyCode"`
	FromLocalDateTime           string  `json:"fromLocalDateTime"`
	IsPrintTicketAllowed        bool    `json:"isPrintTicketAllowed"`
	IsRefundAllowed             bool    `json:"isRefundAllowed"`
	IsSeatTransferred           bool    `json:"isSeatTransferred"`
	IsTicketExtended            bool    `json:"isTicketExtended"`
	PackageID                   string  `json:"packageId"`
	PackageName                 string  `json:"packageName"`
	PackageName2                string  `json:"packageName2"`
	PassengerContactNo          string  `json:"passengerContactNo"`
	PassengerFullName           string  `json:"passengerFullName"`
	PassengerGender             string  `json:"passengerGender"`
	PassengerIdentityNo         string  `json:"passengerIdentityNo"`
	PassengerIndex              int64   `json:"passengerIndex"`
	PassengerPassportExpiryDate string  `json:"passengerPassportExpiryDate"`
	PassengerPassportNo         string  `json:"passengerPassportNo"`
	PassengerPNRNo              string  `json:"passengerPNRNo"`
	PromoAmount                 float32 `json:"promoAmount"`
	PromoCode                   string  `json:"promoCode"`
	SeatNo                      string  `json:"seatNo"`
	SeatTypeName                string  `json:"seatTypeName"`
	ServiceTypeName             string  `json:"serviceTypeName"`
	TicketAmount                float32 `json:"ticketAmount"`
	TicketData                  string  `json:"ticketData"`
	TicketIndex                 int64   `json:"ticketIndex"`
	TicketMinusPromoAmount      float32 `json:"ticketMinusPromoAmount"`
	TicketNo                    string  `json:"ticketNo"`
	TicketStatusLabel           string  `json:"ticketStatusLabel"`
	TicketStatusKey             string  `json:"ticketStatusKey"`
	TicketTypeID                string  `json:"ticketTypeId"`
	TicketTypeName              string  `json:"ticketTypeName"`
	ToLocalDateTime             string  `json:"toLocalDateTime"`
	TotalAddOnAmount            float32 `json:"totalAddOnAmount"`
	TotalAmount                 float32 `json:"totalAmount"`
}

// GetTicketRes
type GetTicketRes struct {
	Bookings              []GetTicketBookingRes `json:"bookings"`
	CompanyAddress        string                `json:"companyAddress"`
	CompanyName           string                `json:"companyName"`
	CompanyRegistrationNo string                `json:"companyRegistrationNo"`
	ErlTicketMessage      interface{}           `json:"erlTicketMessage"`
	TicketMessage         string                `json:"ticketMessage"`
}

type GetTicketBookingRes struct {
	BookingData                       string           `json:"bookingData"`
	BookingLocalDateTime              string           `json:"bookingLocalDateTime"`
	BookingNo                         string           `json:"bookingNo"`
	BookingStatusKey                  string           `json:"bookingStatusKey"`
	BookingStatusLabel                string           `json:"bookingStatusLabel"`
	BookingStatusLabelBackgroundColor string           `json:"bookingStatusLabelBackgroundColor"`
	BookingTypeKey                    string           `json:"bookingTypeKey"`
	CurrencyCode                      string           `json:"currencyCode"`
	DepartFromLocalDateTime           string           `json:"departFromLocalDateTime"`
	DepartToLocalDateTime             string           `json:"departToLocalDateTime"`
	FromStationName                   string           `json:"fromStationName"`
	HasReturn                         bool             `json:"hasReturn"`
	IsPrintTicketAllowed              bool             `json:"isPrintTicketAllowed"`
	IsRefundAllowed                   bool             `json:"isRefundAllowed"`
	OriginalBooking                   interface{}      `json:"originalBooking"`
	PassengerCount                    int64            `json:"passengerCount"`
	Payment                           GetTicketPayment `json:"payment"`
	ReturnFromLocalDateTime           interface{}      `json:"returnFromLocalDateTime"`
	ReturnToLocalDateTime             interface{}      `json:"returnToLocalDateTime"`
	RoundingAmount                    float32          `json:"roundingAmount"`
	TicketAddOnCount                  int64            `json:"ticketAddOnCount"`
	TicketCount                       int64            `json:"ticketCount"`
	ToStationName                     string           `json:"toStationName"`
	TotalAddOnAmount                  float32          `json:"totalAddOnAmount"`
	TotalAmount                       float32          `json:"totalAmount"`
	TotalTicketAmount                 float32          `json:"totalTicketAmount"`
	TripCount                         int64            `json:"tripCount"`
	Trips                             []GetTicketTrip  `json:"trips"`
}

type GetTicketPayment struct {
	BookingPaymentData     string            `json:"bookingPaymentData"`
	CompletedLocalDateTime string            `json:"completedLocalDateTime"`
	CurrencyCode           string            `json:"currencyCode"`
	Details                []GetTicketDetail `json:"details"`
	DiscountAmount         float32           `json:"discountAmount"`
	PaymentAmount          float32           `json:"paymentAmount"`
	PaymentLocalDateTime   string            `json:"paymentLocalDateTime"`
	PaymentNo              string            `json:"paymentNo"`
	PaymentStatusKey       string            `json:"paymentStatusKey"`
	RoundingAmount         float32           `json:"roundingAmount"`
	TotalAddOnAmount       float32           `json:"totalAddOnAmount"`
	TotalBookingAmount     float32           `json:"totalBookingAmount"`
	TotalTicketAmount      float32           `json:"totalTicketAmount"`
}

type GetTicketDetail struct {
	CurrencyCode         string  `json:"currencyCode"`
	DetailData           string  `json:"detailData"`
	PaymentAmount        float32 `json:"paymentAmount"`
	PaymentDetailTypeKey string  `json:"paymentDetailTypeKey"`
	PaymentMethodKey     string  `json:"paymentMethodKey"`
	PaymentMethodName    string  `json:"paymentMethodName"`
	UserEnteredAmount    float32 `json:"userEnteredAmount"`
	UserEnteredText      string  `json:"userEnteredText"`
}

type GetTicketTrip struct {
	AddOns                          []interface{}     `json:"addOns"`
	CurrencyCode                    string            `json:"currencyCode"`
	FromLocalDateTime               string            `json:"fromLocalDateTime"`
	FromStationName                 string            `json:"fromStationName"`
	IsCanceled                      bool              `json:"isCanceled"`
	IsRefundAllowed                 bool              `json:"isRefundAllowed"`
	IsReturn                        bool              `json:"isReturn"`
	OriginalTrip                    interface{}       `json:"originalTrip"`
	Tickets                         []GetTicketTicket `json:"tickets"`
	ToLocalDateTime                 string            `json:"toLocalDateTime"`
	ToStationName                   string            `json:"toStationName"`
	TotalAddOnAmount                float32           `json:"totalAddOnAmount"`
	TotalAmount                     float32           `json:"totalAmount"`
	TotalInsuranceAddOnAmount       float32           `json:"totalInsuranceAddOnAmount"`
	TotalTicketAmount               float32           `json:"totalTicketAmount"`
	TotalTicketInsuranceAddOnAmount float32           `json:"totalTicketInsuranceAddOnAmount"`
	TrainService                    string            `json:"trainService"`
	TrainServiceCategory            string            `json:"trainServiceCategory"`
	TrainServiceLabel               string            `json:"trainServiceLabel"`
	TripBorderLeftColor             interface{}       `json:"tripBorderLeftColor"`
	TripData                        string            `json:"tripData"`
	TripIndex                       int64             `json:"tripIndex"`
	TripLabelTagText                interface{}       `json:"tripLabelTagText"`
	TripName                        string            `json:"tripName"`
	TripNo                          string            `json:"tripNo"`
	TripStatusKey                   string            `json:"tripStatusKey"`
	TripStatusLabel                 string            `json:"tripStatusLabel"`
}

type GetTicketTicket struct {
	AddOns                      []interface{} `json:"addOns"`
	BoardingCode                string        `json:"boardingCode"`
	CoachName                   string        `json:"coachName"`
	CoachNameFrom               interface{}   `json:"coachNameFrom"`
	CurrencyCode                string        `json:"currencyCode"`
	FromLocalDateTime           string        `json:"fromLocalDateTime"`
	InsuranceAddOn              interface{}   `json:"insuranceAddOn"`
	IsPrintTicketAllowed        bool          `json:"isPrintTicketAllowed"`
	IsRefundAllowed             bool          `json:"isRefundAllowed"`
	IsSeatTransferred           bool          `json:"isSeatTransferred"`
	IsTicketExtended            bool          `json:"isTicketExtended"`
	NewBookingNo                interface{}   `json:"newBookingNo"`
	OriginalTicket              interface{}   `json:"originalTicket"`
	PackageID                   string        `json:"packageId"`
	PackageName                 string        `json:"packageName"`
	PackageName2                string        `json:"packageName2"`
	PassengerContactNo          string        `json:"passengerContactNo"`
	PassengerFullName           string        `json:"passengerFullName"`
	PassengerGender             string        `json:"passengerGender"`
	PassengerIdentityNo         string        `json:"passengerIdentityNo"`
	PassengerIndex              int64         `json:"passengerIndex"`
	PassengerPNRNo              string        `json:"passengerPNRNo"`
	PassengerPassportExpiryDate string        `json:"passengerPassportExpiryDate"`
	PassengerPassportNo         string        `json:"passengerPassportNo"`
	PromoAmount                 float32       `json:"promoAmount"`
	PromoCode                   string        `json:"promoCode"`
	SeatNo                      string        `json:"seatNo"`
	SeatNoFrom                  interface{}   `json:"seatNoFrom"`
	SeatTypeName                string        `json:"seatTypeName"`
	ServiceTypeName             string        `json:"serviceTypeName"`
	TicketAmount                float32       `json:"ticketAmount"`
	TicketData                  string        `json:"ticketData"`
	TicketIndex                 int64         `json:"ticketIndex"`
	TicketMinusPromoAmount      float32       `json:"ticketMinusPromoAmount"`
	TicketNo                    string        `json:"ticketNo"`
	TicketStatusKey             string        `json:"ticketStatusKey"`
	TicketStatusLabel           string        `json:"ticketStatusLabel"`
	TicketTypeID                string        `json:"ticketTypeId"`
	TicketTypeName              string        `json:"ticketTypeName"`
	ToLocalDateTime             string        `json:"toLocalDateTime"`
	TotalAddOnAmount            float32       `json:"totalAddOnAmount"`
	TotalAmount                 float32       `json:"totalAmount"`
}

// TicketListRes

type TicketListRes struct {
	Bookings          []TicketListBookingRes `json:"bookings"`
	IsAcceptPdpa      bool                   `json:"isAcceptPdpa"`
	Page              int64                  `json:"page"`
	RefundPassword    interface{}            `json:"refundPassword"`
	ShareBookingEmail interface{}            `json:"shareBookingEmail"`
	ShareReceiptEmail interface{}            `json:"shareReceiptEmail"`
	TotalCount        int64                  `json:"totalCount"`
	TotalPage         int64                  `json:"totalPage"`
}

type TicketListBookingRes struct {
	BookingData                       string               `json:"bookingData"`
	BookingLocalDateTime              string               `json:"bookingLocalDateTime"`
	BookingNo                         string               `json:"bookingNo"`
	BookingStatusKey                  string               `json:"bookingStatusKey"`
	BookingStatusLabel                string               `json:"bookingStatusLabel"`
	BookingStatusLabelBackgroundColor string               `json:"bookingStatusLabelBackgroundColor"`
	BookingTypeKey                    string               `json:"bookingTypeKey"`
	CurrencyCode                      string               `json:"currencyCode"`
	DepartFromLocalDateTime           string               `json:"departFromLocalDateTime"`
	DepartToLocalDateTime             string               `json:"departToLocalDateTime"`
	FromStationName                   string               `json:"fromStationName"`
	HasReturn                         bool                 `json:"hasReturn"`
	IsPrintTicketAllowed              bool                 `json:"isPrintTicketAllowed"`
	IsRefundAllowed                   bool                 `json:"isRefundAllowed"`
	OriginalBooking                   interface{}          `json:"originalBooking"`
	PassengerCount                    int64                `json:"passengerCount"`
	Payment                           TicketListPaymentRes `json:"payment"`
	ReturnFromLocalDateTime           interface{}          `json:"returnFromLocalDateTime"`
	ReturnToLocalDateTime             interface{}          `json:"returnToLocalDateTime"`
	RoundingAmount                    float32              `json:"roundingAmount"`
	TicketAddOnCount                  int64                `json:"ticketAddOnCount"`
	TicketCount                       int64                `json:"ticketCount"`
	ToStationName                     string               `json:"toStationName"`
	TotalAddOnAmount                  float32              `json:"totalAddOnAmount"`
	TotalAmount                       float32              `json:"totalAmount"`
	TotalTicketAmount                 float32              `json:"totalTicketAmount"`
	TripCount                         int64                `json:"tripCount"`
	Trips                             []TicketListTripRes  `json:"trips"`
}

type TicketListPaymentRes struct {
	BookingPaymentData     string                `json:"bookingPaymentData"`
	CompletedLocalDateTime string                `json:"completedLocalDateTime"`
	CurrencyCode           string                `json:"currencyCode"`
	Details                []TicketListDetailRes `json:"details"`
	DiscountAmount         float32               `json:"discountAmount"`
	PaymentAmount          float32               `json:"paymentAmount"`
	PaymentLocalDateTime   string                `json:"paymentLocalDateTime"`
	PaymentNo              string                `json:"paymentNo"`
	PaymentStatusKey       string                `json:"paymentStatusKey"`
	RoundingAmount         float32               `json:"roundingAmount"`
	TotalAddOnAmount       float32               `json:"totalAddOnAmount"`
	TotalBookingAmount     float32               `json:"totalBookingAmount"`
	TotalTicketAmount      float32               `json:"totalTicketAmount"`
}

type TicketListDetailRes struct {
	CurrencyCode         string      `json:"currencyCode"`
	DetailData           string      `json:"detailData"`
	PaymentAmount        float32     `json:"paymentAmount"`
	PaymentDetailTypeKey string      `json:"paymentDetailTypeKey"`
	PaymentMethodKey     string      `json:"paymentMethodKey"`
	PaymentMethodName    interface{} `json:"paymentMethodName"`
	UserEnteredAmount    float32     `json:"userEnteredAmount"`
	UserEnteredText      string      `json:"userEnteredText"`
}

type TicketListTripRes struct {
	AddOns                          []interface{}         `json:"addOns"`
	CurrencyCode                    string                `json:"currencyCode"`
	FromLocalDateTime               string                `json:"fromLocalDateTime"`
	FromStationName                 string                `json:"fromStationName"`
	IsCanceled                      bool                  `json:"isCanceled"`
	IsRefundAllowed                 bool                  `json:"isRefundAllowed"`
	IsReturn                        bool                  `json:"isReturn"`
	OriginalTrip                    interface{}           `json:"originalTrip"`
	Tickets                         []TicketListTicketRes `json:"tickets"`
	ToLocalDateTime                 string                `json:"toLocalDateTime"`
	ToStationName                   string                `json:"toStationName"`
	TotalAddOnAmount                float32               `json:"totalAddOnAmount"`
	TotalAmount                     float32               `json:"totalAmount"`
	TotalInsuranceAddOnAmount       float32               `json:"totalInsuranceAddOnAmount"`
	TotalTicketAmount               float32               `json:"totalTicketAmount"`
	TotalTicketInsuranceAddOnAmount float32               `json:"totalTicketInsuranceAddOnAmount"`
	TrainService                    string                `json:"trainService"`
	TrainServiceCategory            string                `json:"trainServiceCategory"`
	TrainServiceLabel               string                `json:"trainServiceLabel"`
	TripBorderLeftColor             interface{}           `json:"tripBorderLeftColor"`
	TripData                        string                `json:"tripData"`
	TripIndex                       int64                 `json:"tripIndex"`
	TripLabelTagText                interface{}           `json:"tripLabelTagText"`
	TripName                        string                `json:"tripName"`
	TripNo                          string                `json:"tripNo"`
	TripStatusKey                   string                `json:"tripStatusKey"`
	TripStatusLabel                 string                `json:"tripStatusLabel"`
}

type TicketListTicketRes struct {
	AddOns                      []interface{} `json:"addOns"`
	BoardingCode                string        `json:"boardingCode"`
	CoachName                   string        `json:"coachName"`
	CoachNameFrom               interface{}   `json:"coachNameFrom"`
	CurrencyCode                string        `json:"currencyCode"`
	FromLocalDateTime           string        `json:"fromLocalDateTime"`
	InsuranceAddOn              interface{}   `json:"insuranceAddOn"`
	IsPrintTicketAllowed        bool          `json:"isPrintTicketAllowed"`
	IsRefundAllowed             bool          `json:"isRefundAllowed"`
	IsSeatTransferred           bool          `json:"isSeatTransferred"`
	IsTicketExtended            bool          `json:"isTicketExtended"`
	NewBookingNo                interface{}   `json:"newBookingNo"`
	OriginalTicket              interface{}   `json:"originalTicket"`
	PackageID                   string        `json:"packageId"`
	PackageName                 string        `json:"packageName"`
	PackageName2                string        `json:"packageName2"`
	PassengerContactNo          string        `json:"passengerContactNo"`
	PassengerFullName           string        `json:"passengerFullName"`
	PassengerGender             string        `json:"passengerGender"`
	PassengerIdentityNo         string        `json:"passengerIdentityNo"`
	PassengerIndex              int64         `json:"passengerIndex"`
	PassengerPNRNo              string        `json:"passengerPNRNo"`
	PassengerPassportExpiryDate string        `json:"passengerPassportExpiryDate"`
	PassengerPassportNo         string        `json:"passengerPassportNo"`
	PromoAmount                 float32       `json:"promoAmount"`
	PromoCode                   string        `json:"promoCode"`
	SeatNo                      string        `json:"seatNo"`
	SeatNoFrom                  interface{}   `json:"seatNoFrom"`
	SeatTypeName                string        `json:"seatTypeName"`
	ServiceTypeName             string        `json:"serviceTypeName"`
	TicketAmount                float32       `json:"ticketAmount"`
	TicketData                  string        `json:"ticketData"`
	TicketIndex                 int64         `json:"ticketIndex"`
	TicketMinusPromoAmount      float32       `json:"ticketMinusPromoAmount"`
	TicketNo                    string        `json:"ticketNo"`
	TicketStatusKey             string        `json:"ticketStatusKey"`
	TicketStatusLabel           string        `json:"ticketStatusLabel"`
	TicketTypeID                string        `json:"ticketTypeId"`
	TicketTypeName              string        `json:"ticketTypeName"`
	ToLocalDateTime             string        `json:"toLocalDateTime"`
	TotalAddOnAmount            float32       `json:"totalAddOnAmount"`
	TotalAmount                 float32       `json:"totalAmount"`
}

// RefundPolicyRes
type RefundPolicyRes struct {
	BookingData       string                `json:"bookingData"`
	CurrencyCode      string                `json:"currencyCode"`
	IsRefundAllowed   bool                  `json:"isRefundAllowed"`
	TotalRefundAmount float32               `json:"totalRefundAmount"`
	Trips             []RefundPolicyTripRes `json:"trips"`
}

type RefundPolicyTripRes struct {
	FromLocalDateTime string                      `json:"fromLocalDateTime"`
	FromStationName   string                      `json:"fromStationName"`
	Tickets           []RefundPolicyTripTicketRes `json:"tickets"`
	ToLocalDateTime   string                      `json:"toLocalDateTime"`
	ToStationName     string                      `json:"toStationName"`
	TripName          string                      `json:"tripName"`
}

type RefundPolicyTripTicketRes struct {
	AddOns                 []interface{} `json:"addOns"`
	CurrencyCode           string        `json:"currencyCode"`
	InsuranceAddOn         interface{}   `json:"insuranceAddOn"`
	PassengerFullName      string        `json:"passengerFullName"`
	RefundAmount           float32       `json:"refundAmount"`
	RefundPercentage       float32       `json:"refundPercentage"`
	TicketData             string        `json:"ticketData"`
	TicketMinusPromoAmount float32       `json:"ticketMinusPromoAmount"`
	TicketNo               string        `json:"ticketNo"`
	TotalAmount            float32       `json:"totalAmount"`
}
