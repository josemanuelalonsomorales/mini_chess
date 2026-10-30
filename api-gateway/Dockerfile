FROM golang:1.27.3-alpine AS builder

WORKDIR /app

COPY proto ./proto
COPY api-gateway ./api-gateway

WORKDIR /app/api-gateway

RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux go build -o gateway .

FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/api-gateway/gateway .
COPY --from=builder /app/api-gateway/public ./public
COPY --from=builder /app/api-gateway/routes.json ./routes.json

EXPOSE 8001

CMD ["./gateway"]
