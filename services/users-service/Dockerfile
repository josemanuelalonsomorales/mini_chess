
FROM golang:1.27.3-alpine AS builder

WORKDIR /app

# Copiar el módulo protobuf compartido y el servicio de usuarios.
COPY proto ./proto
COPY services/users-service ./services/users-service

WORKDIR /app/services/users-service

RUN go mod download

# Compilar el servidor para Linux.
RUN CGO_ENABLED=0 GOOS=linux go build -o users-service .

# Imagen final ligera.
FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/services/users-service/users-service .

EXPOSE 50054

CMD ["./users-service"]
