﻿FROM golang:1.21.4-alpine3.18 as builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/nitroso-tin

FROM alpine:3.18
RUN apk add --no-cache tzdata=2023c-r1 ca-certificates=20230506-r0 && update-ca-certificates
WORKDIR /app
COPY --from=builder /app/nitroso-tin /app/nitroso-tin

ENTRYPOINT [ "/app/nitroso-tin" ]
CMD [ "cdc" ]