package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/segmentio/kafka-go"
)

type userEvent struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	UserID    int32  `json:"user_id"`
	Username  string `json:"username"`
	CreatedAt string `json:"created_at"`
}

type gameEvent struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	GameID    int32  `json:"game_id"`
	Instance  string `json:"instance_id"`
	Details   string `json:"details"`
	CreatedAt string `json:"created_at"`
}

// handleUserEvent procesa eventos del tópico users.events.
func handleUserEvent(ctx context.Context, msg kafka.Message) error {
	var event userEvent

	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return fmt.Errorf("error decodificando evento de usuario: %w", err)
	}

	if event.EventType != "user.registered" {
		return fmt.Errorf("tipo de evento de usuario no reconocido: %s", event.EventType)
	}

	if event.UserID <= 0 || event.Username == "" {
		return fmt.Errorf("evento de usuario incompleto")
	}

	log.Printf(
		"Usuario registrado procesado: ID=%d, username=%s, event_id=%s",
		event.UserID,
		event.Username,
		event.EventID,
	)

	return nil
}

// handleGameEvent procesa eventos del tópico games.events.
func handleGameEvent(ctx context.Context, msg kafka.Message) error {
	var event gameEvent

	if err := json.Unmarshal(msg.Value, &event); err != nil {
		return fmt.Errorf("error decodificando evento de partida: %w", err)
	}

	if event.GameID <= 0 {
		return fmt.Errorf("ID de partida inválido")
	}

	switch event.EventType {
	case "game.move_completed":
		log.Printf(
			"Movimiento procesado: partida=%d, instancia=%s, detalles=%s",
			event.GameID,
			event.Instance,
			event.Details,
		)

	case "game.reset":
		log.Printf(
			"Reinicio procesado: partida=%d, instancia=%s",
			event.GameID,
			event.Instance,
		)

	default:
		return fmt.Errorf("tipo de evento de partida no reconocido: %s", event.EventType)
	}

	return nil
}
