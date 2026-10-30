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

const gamesTopic = "games.events"

type gameEvent struct {
	EventID   string    `json:"event_id"`
	EventType string    `json:"event_type"`
	GameID    int32     `json:"game_id"`
	Instance  string    `json:"instance_id"`
	Details   string    `json:"details,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type gameKafkaPublisher struct {
	writer *kafka.Writer
	queue  chan gameEvent
	done   chan struct{}
}

func newGameKafkaPublisher() *gameKafkaPublisher {
	brokers := strings.TrimSpace(os.Getenv("KAFKA_BROKERS"))
	if brokers == "" {
		brokers = "kafka:9092"
	}

	p := &gameKafkaPublisher{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(strings.Split(brokers, ",")...),
			Topic:        gamesTopic,
			Balancer:     &kafka.LeastBytes{},
			RequiredAcks: kafka.RequireOne,
			Async:        false,
		},
		queue: make(chan gameEvent, 100),
		done:  make(chan struct{}),
	}

	go p.run()

	log.Println("Productor Kafka preparado para el tópico:", gamesTopic)

	return p
}

func (p *gameKafkaPublisher) publish(eventType string, gameID int32, details string) {
	event := gameEvent{
		EventID:   time.Now().UTC().Format("20060102T150405.000000000") + "-" + instanceID,
		EventType: eventType,
		GameID:    gameID,
		Instance:  instanceID,
		Details:   details,
		CreatedAt: time.Now().UTC(),
	}

	select {
	case p.queue <- event:
		log.Printf("Evento %s encolado para partida %d", eventType, gameID)
	default:
		log.Printf("Cola Kafka llena; se descartó el evento %s de partida %d", eventType, gameID)
	}
}

func (p *gameKafkaPublisher) run() {
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
				log.Printf("Evento %s publicado para partida %d", event.EventType, event.GameID)
				break
			}

			log.Printf("Error publicando evento Kafka (%d/3): %v", attempt, err)
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		if err != nil {
			log.Printf("No se pudo publicar el evento %s de partida %d", event.EventType, event.GameID)
		}
	}
}

func (p *gameKafkaPublisher) close() {
	close(p.queue)
	<-p.done

	if err := p.writer.Close(); err != nil {
		log.Printf("Error cerrando productor Kafka: %v", err)
	}
}
