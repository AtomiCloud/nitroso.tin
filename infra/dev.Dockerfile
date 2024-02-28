FROM golang:1.21.6

WORKDIR /app

RUN go install github.com/cosmtrek/air@v1.49.0

COPY go.mod go.sum ./

RUN go mod download && go mod verify

COPY . .

CMD [ "air", "--", "cdc" ]