FROM golang:1.21.4-alpine3.18 as builder

WORKDIR /app

RUN go install github.com/cosmtrek/air@latest

COPY go.mod go.sum ./

RUN go mod download && go mod verify

COPY . .

CMD [ "air", "--", "start" ]