package ktmb

import "github.com/rs/zerolog"

type Ktmb struct {
	ApiUrl    string
	AppUrl    string
	Signature string
	logger    *zerolog.Logger
	proxy     *string
}

func New(apiUrl, appUrl string, ktmbSignature string, logger *zerolog.Logger, proxy *string) Ktmb {

	var p *string
	if proxy != nil {
		p = proxy
		if *p == "" {
			p = nil
		}
	} else {
		p = nil
	}

	return Ktmb{
		ApiUrl:    apiUrl,
		AppUrl:    appUrl,
		Signature: ktmbSignature,
		logger:    logger,
		proxy:     p,
	}
}

var apiHost = map[string]string{
	"Host": "shuttleonline-api.ktmb.com.my",
}

var appHost = map[string]string{
	"Host": "online-api.ktmb.com.my",
}

func (k *Ktmb) NewApi() HttpConfig {
	return HttpConfig{
		Url: k.ApiUrl,
		Header: map[string]string{
			"Content-Type":     "application/json",
			"requestSignature": k.Signature,
		},
		logger: k.logger,
		proxy:  k.proxy,
	}
}

func (k *Ktmb) NewApp() HttpConfig {
	return HttpConfig{
		Url: k.AppUrl,
		Header: map[string]string{
			"Content-Type":     "application/json",
			"requestSignature": k.Signature,
		},
		logger: k.logger,
		proxy:  k.proxy,
	}
}

func (k *Ktmb) Login(email string, password string) (GenericRes[LoginRes], error) {
	client := NewHttp[LoginReq, GenericRes[LoginRes]](k.NewApi())
	req := LoginReq{
		Email:    email,
		Password: password,
	}
	r, err := client.SendWith("POST", "v1/account/EmailLogin", req)
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to login")
		return GenericRes[LoginRes]{}, err
	}
	return r, nil
}

func (k *Ktmb) StationsAll(userData string) (GenericRes[StationsAllRes], error) {
	client := NewHttp[string, GenericRes[StationsAllRes]](k.NewApp())
	r, err := client.Send("POST", "/v1/shuttletrip/Station", apiHost, map[string]string{
		"userData": userData,
	})
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to get all stations")
		return GenericRes[StationsAllRes]{}, err
	}
	return r, nil
}

func (k *Ktmb) SearchStations(userData, fromStationId, fromStationData, toStationId, toStationData, onwardDate string, passengerCount int64) (GenericRes[SearchStationsRes], error) {
	client := NewHttp[SearchReq, GenericRes[SearchStationsRes]](k.NewApp())
	req := SearchReq{
		FromStationData: fromStationData,
		FromStationID:   fromStationId,
		OnwardDate:      onwardDate,
		PassengerCount:  passengerCount,
		ToStationData:   toStationData,
		ToStationID:     toStationId,
	}
	r, err := client.SendWith("POST", "/v1/shuttletrip/Search", req, apiHost, map[string]string{
		"userData": userData,
	})
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to get search stations")
		return GenericRes[SearchStationsRes]{}, err
	}
	return r, nil
}

func (k *Ktmb) Trip(userData, departDate, searchData string) (GenericRes[TripAllRes], error) {
	client := NewHttp[TripReq, GenericRes[TripAllRes]](k.NewApp())
	req := TripReq{
		BookingTripSequenceNo: 1,
		DepartDate:            departDate,
		SearchData:            searchData,
	}
	r, err := client.SendWith("POST", "/v1/shuttletrip/Trip", req, apiHost, map[string]string{
		"userData": userData,
	})
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to fetch trips")
		return GenericRes[TripAllRes]{}, err
	}
	return r, nil
}

func (k *Ktmb) Reserve(userData, appInformation, searchData, tripData string) (GenericRes[ReserveRes], error) {
	client := NewHttp[ReserveReq, GenericRes[ReserveRes]](k.NewApp())
	req := ReserveReq{
		AppInformation: appInformation,
		SearchData:     searchData,
		Trips: []Trip{
			{
				TripData: tripData,
			},
		},
	}
	r, err := client.SendWith("POST", "/v1/shuttletrip/Reserve", req, apiHost, map[string]string{
		"userData": userData,
	})
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to reserve")
		return GenericRes[ReserveRes]{}, err
	}
	return r, nil
}

func (k *Ktmb) BookStart(userData, bookingData string) (GenericRes[BookStartRes], error) {
	client := NewHttp[BookStartReq, GenericRes[BookStartRes]](k.NewApp())

	req := BookStartReq{
		BookingData: bookingData,
	}

	r, err := client.SendWith("POST", "/v1/bookshuttle/GetBookingForUpdate", req, apiHost, map[string]string{
		"userData": userData,
	})
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to book start")
		return GenericRes[BookStartRes]{}, err
	}
	return r, nil
}

func (k *Ktmb) SetPassenger(userData, bookingData string, passengers []PassengerReq) (GenericRes[SetPassengerRes], error) {
	client := NewHttp[SetPassengerReq, GenericRes[SetPassengerRes]](k.NewApp())

	req := SetPassengerReq{
		BookingData: bookingData,
		Passengers:  passengers,
	}

	r, err := client.SendWith("POST", "/v1/bookshuttle/UpdatePassenger", req, apiHost, map[string]string{
		"userData": userData,
	})
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to set passenger")
		return GenericRes[SetPassengerRes]{}, err
	}
	return r, nil
}

func (k *Ktmb) Pay(userData, bookingData, method string, amount float32) (GenericRes[PaymentRes], error) {
	client := NewHttp[PayReq, GenericRes[PaymentRes]](k.NewApp())

	req := PayReq{
		BookingData:    bookingData,
		DiscountAmount: 0,
		EWalletAmount:  0,
		PaymentAmount:  amount,
		PaymentMethod:  method,
		TotalAmount:    amount,
	}

	r, err := client.SendWith("POST", "/v1/bookshuttle/UpdatePayment", req, apiHost, map[string]string{
		"userData": userData,
	})
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to pay")
		return GenericRes[PaymentRes]{}, err
	}
	return r, nil
}

func (k *Ktmb) Complete(userData, bookingData string) (GenericRes[CompleteRes], error) {
	client := NewHttp[CompleteReq, GenericRes[CompleteRes]](k.NewApp())

	req := CompleteReq{
		BookingData: bookingData,
	}

	r, err := client.SendWith("POST", "/v1/bookshuttle/Summary", req, apiHost, map[string]string{
		"userData": userData,
	})
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to complete")
		return GenericRes[CompleteRes]{}, err
	}
	return r, nil
}

func (k *Ktmb) PrintTicket(userData, bookingNo, ticketNo string) ([]byte, error) {
	client := NewHttp[PrintTicketReq, string](k.NewApi())

	req := PrintTicketReq{
		BookingNo: bookingNo,
		Tickets: []PrintTicketTicketReq{
			{
				TicketNo: ticketNo,
			},
		},
	}
	r, err := client.BinarySendWith("POST", "/v1/booking/PrintTicketPdf", req, appHost, map[string]string{
		"userData": userData,
	})
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to print ticket")
		return r, err
	}
	return r, nil
}

func (k *Ktmb) Cancel(userData, bookingData string) (GenericRes[*interface{}], error) {
	client := NewHttp[CancelReserveReq, GenericRes[*interface{}]](k.NewApp())

	req := CancelReserveReq{
		BookingData: bookingData,
	}
	r, err := client.SendWith("POST", "/v1/bookshuttle/Cancel", req, apiHost, map[string]string{
		"userData": userData,
	})
	if err != nil {
		k.logger.Error().Err(err).Msg("Failed to print ticket")
		return GenericRes[*interface{}]{}, err
	}
	return r, nil
}
