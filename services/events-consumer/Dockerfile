FROM golang:1.27.3-alpine AS builder

WORKDIR /app

COPY services/events-consumer ./services/events-consumer

WORKDIR /app/services/events-consumer

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -o events-consumer .

FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/services/events-consumer/events-consumer .

CMD ["./events-consumer"]
