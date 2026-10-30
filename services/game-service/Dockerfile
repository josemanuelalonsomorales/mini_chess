FROM golang:1.27.3-alpine AS builder

WORKDIR /app

COPY proto ./proto
COPY services/game-service ./services/game-service

WORKDIR /app/services/game-service

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -o game-service .

FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/services/game-service/game-service .

EXPOSE 50053

CMD ["./game-service"]
