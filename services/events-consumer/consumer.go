package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/segmentio/kafka-go"
)

// Handler procesa un mensaje recibido de Kafka.
type Handler func(ctx context.Context, msg kafka.Message) error

// Consume se suscribe a un tópico mediante un grupo de consumidores.
func Consume(
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
	handler Handler,
) error {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokers,
		Topic:       topic,
		GroupID:     groupID,
		StartOffset: kafka.FirstOffset,
		MinBytes:    1,
		MaxBytes:    10e6,
	})

	defer reader.Close()

	log.Printf(
		"Consumidor iniciado: tópico=%s, grupo=%s",
		topic,
		groupID,
	)

	for {
		msg, err := reader.FetchMessage(ctx)

		if err != nil {
			if errors.Is(err, context.Canceled) {
				log.Printf("Consumidor detenido: %s", topic)
				return nil
			}
			return err
		}

		log.Printf(
			"Mensaje recibido: tópico=%s, partición=%d, offset=%d",
			msg.Topic,
			msg.Partition,
			msg.Offset,
		)

		// Procesar antes de confirmar el offset.
		if err := processWithRetry(ctx, handler, msg, 3); err != nil {
			log.Printf(
				"Mensaje no procesado; offset sin confirmar: %v",
				err,
			)
			return err
		}

		// Confirmar únicamente después del procesamiento exitoso.
		if err := reader.CommitMessages(ctx, msg); err != nil {
			return err
		}

		log.Printf(
			"Mensaje confirmado: tópico=%s, offset=%d",
			msg.Topic,
			msg.Offset,
		)
	}
}

// processWithRetry reintenta el procesamiento hasta alcanzar el límite.
func processWithRetry(
	ctx context.Context,
	handler Handler,
	msg kafka.Message,
	attempts int,
) error {
	var err error

	for i := 1; i <= attempts; i++ {
		if err = handler(ctx, msg); err == nil {
			return nil
		}

		log.Printf(
			"Intento %d/%d fallido: %v",
			i,
			attempts,
			err,
		)

		if i < attempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(i) * 500 * time.Millisecond):
			}
		}
	}

	return err
}
