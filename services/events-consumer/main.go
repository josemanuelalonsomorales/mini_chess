package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	brokersEnv := strings.TrimSpace(os.Getenv("KAFKA_BROKERS"))

	if brokersEnv == "" {
		brokersEnv = "kafka:9092"
	}

	brokers := strings.Split(brokersEnv, ",")

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	log.Println("Events Consumer iniciando...")
	log.Printf("Brokers Kafka: %s", brokersEnv)

	errCh := make(chan error, 2)

	// Consumidor de eventos de usuarios.
	go func() {
		errCh <- Consume(
			ctx,
			brokers,
			"users.events",
			"users-events-group",
			handleUserEvent,
		)
	}()

	// Consumidor de eventos de partidas.
	go func() {
		errCh <- Consume(
			ctx,
			brokers,
			"games.events",
			"games-events-group",
			handleGameEvent,
		)
	}()

	// Esperar a que termine algún consumidor.
	err := <-errCh

	if err != nil {
		log.Printf("Error en consumidor: %v", err)
	} else {
		log.Println("Consumidor detenido correctamente")
	}

	stop()

	// Esperar a que termine el segundo consumidor.
	<-errCh

	log.Println("Events Consumer finalizado")
}
