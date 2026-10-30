
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

const usersTopic = "users.events"

type userRegisteredEvent struct {
	EventID   string    `json:"event_id"`
	EventType string    `json:"event_type"`
	UserID    int32     `json:"user_id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

type kafkaPublisher struct {
	writer *kafka.Writer
	queue  chan userRegisteredEvent
	done   chan struct{}
}

func newKafkaPublisher() *kafkaPublisher {
	brokers := strings.TrimSpace(os.Getenv("KAFKA_BROKERS"))
	if brokers == "" {
		brokers = "kafka:9092"
	}

	p := &kafkaPublisher{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(strings.Split(brokers, ",")...),
			Topic:        usersTopic,
			Balancer:     &kafka.LeastBytes{},
			RequiredAcks: kafka.RequireOne,
			Async:        false,
		},
		queue: make(chan userRegisteredEvent, 100),
		done:  make(chan struct{}),
	}

	go p.run()
	log.Println("Productor Kafka preparado para el tópico:", usersTopic)
	return p
}

func (p *kafkaPublisher) publishUserRegistered(userID int32, username string) {
	event := userRegisteredEvent{
		EventID:   time.Now().UTC().Format("20060102T150405.000000000") + "-" + strings.TrimSpace(username),
		EventType: "user.registered",
		UserID:    userID,
		Username:  username,
		CreatedAt: time.Now().UTC(),
	}

	select {
	case p.queue <- event:
		log.Printf("Evento user.registered encolado para usuario ID %d", userID)
	default:
		log.Printf("Cola Kafka llena; se descartó el evento user.registered del usuario ID %d", userID)
	}
}

func (p *kafkaPublisher) run() {
	defer close(p.done)

	for event := range p.queue {
		payload, err := json.Marshal(event)
		if err != nil {
			log.Printf("Error serializando evento Kafka: %v", err)
			continue
		}

		msg := kafka.Message{
			Key:   []byte(event.EventID),
			Value: payload,
			Time:  event.CreatedAt,
		}

		for attempt := 1; attempt <= 3; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			err = p.writer.WriteMessages(ctx, msg)
			cancel()

			if err == nil {
				log.Printf("Evento user.registered publicado para usuario ID %d", event.UserID)
				break
			}

			log.Printf("Error publicando evento Kafka (%d/3): %v", attempt, err)
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		if err != nil {
			log.Printf("No se pudo publicar el evento del usuario ID %d", event.UserID)
		}
	}
}

func (p *kafkaPublisher) close() {
	close(p.queue)
	<-p.done

	if err := p.writer.Close(); err != nil {
		log.Printf("Error cerrando productor Kafka: %v", err)
	}
}
