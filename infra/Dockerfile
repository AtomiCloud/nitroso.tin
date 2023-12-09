FROM golang:1.21.4-alpine3.18 as builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/nitroso-tin

FROM alpine:3.18
WORKDIR /app
COPY --from=builder /app/nitroso-tin /app/nitroso-tin

ENTRYPOINT [ "/app/nitroso-tin" ]
CMD [ "start" ]