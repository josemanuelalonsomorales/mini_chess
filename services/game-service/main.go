package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"

	_ "github.com/lib/pq"
)

var conexionBaseDatos *sql.DB
var instanceID string

type Juego struct {
	ID          int     `json:"id"`
	TurnoActual string  `json:"turno_actual"`
	Estado      string  `json:"estado"`
	Piezas      []Pieza `json:"piezas,omitempty"`
}

type Pieza struct {
	ID        int    `json:"id"`
	IDJuego   int    `json:"id_juego"`
	Jugador   string `json:"jugador"`
	Tipo      string `json:"tipo"`
	PosicionX string `json:"pos_x"`
	PosicionY int    `json:"pos_y"`
	EstaViva  bool   `json:"esta_viva"`
}

type SolicitudMovimiento struct {
	IDPieza  int    `json:"id_pieza"`
	DestinoX string `json:"destino_x"`
	DestinoY int    `json:"destino_y"`
}

func main() {
	instanceID = getEnv("INSTANCE_ID", "backend-local")
	dbHost := getEnv("DB_HOST", "localhost")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "postgres")
	dbPassword := getEnv("DB_PASSWORD", "sh0ut1nk")
	dbName := getEnv("DB_NAME", "chess_db")

	cadenaConexion := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	var err error
	conexionBaseDatos, err = sql.Open("postgres", cadenaConexion)
	if err != nil {
		log.Fatalf("[Game Service %s] Error al abrir conexión con BD: %v", instanceID, err)
	}
	defer conexionBaseDatos.Close()

	if err = conexionBaseDatos.Ping(); err != nil {
		log.Fatalf("[Game Service %s] No se pudo conectar a la BD: %v", instanceID, err)
	}

	http.HandleFunc("/heartbeat", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Instance-Id", instanceID)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	http.HandleFunc("/api/juego", instanceMiddleware(logMiddleware(getEstadoJuego)))
	http.HandleFunc("/api/mover", instanceMiddleware(logMiddleware(hacerMovimiento)))
	http.HandleFunc("/api/reiniciar", instanceMiddleware(logMiddleware(reiniciarJuego)))

	puerto := getEnv("PORT", "8080")
	log.Printf("[Game Service: %s] Microservicio corriendo en puerto %s", instanceID, puerto)
	log.Fatal(http.ListenAndServe(":"+puerto, nil))
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func obtenerIDJuego(r *http.Request) int {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		return 1
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return 1
	}
	return id
}

func getEstadoJuego(w http.ResponseWriter, r *http.Request) {
	gameID := obtenerIDJuego(r)

	var juegoActual Juego
	err := conexionBaseDatos.QueryRow("SELECT id, current_turn, status FROM games WHERE id = $1", gameID).
		Scan(&juegoActual.ID, &juegoActual.TurnoActual, &juegoActual.Estado)
	if err != nil {
		http.Error(w, "Error al consultar juego", http.StatusInternalServerError)
		return
	}

	filasObtenidas, err := conexionBaseDatos.Query(
		"SELECT id, game_id, player, type, pos_x, pos_y, is_alive FROM pieces WHERE game_id = $1 AND is_alive = TRUE", gameID)
	if err != nil {
		http.Error(w, "Error al consultar piezas", http.StatusInternalServerError)
		return
	}
	defer filasObtenidas.Close()

	for filasObtenidas.Next() {
		var piezaActual Pieza
		err = filasObtenidas.Scan(&piezaActual.ID, &piezaActual.IDJuego, &piezaActual.Jugador, &piezaActual.Tipo, &piezaActual.PosicionX, &piezaActual.PosicionY, &piezaActual.EstaViva)
		if err != nil {
			continue
		}
		juegoActual.Piezas = append(juegoActual.Piezas, piezaActual)
	}
	if err = filasObtenidas.Err(); err != nil {
		http.Error(w, "Error al recorrer piezas", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(juegoActual)
}

func hacerMovimiento(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	gameID := obtenerIDJuego(r)

	var datosSolicitud SolicitudMovimiento
	err := json.NewDecoder(r.Body).Decode(&datosSolicitud)
	if err != nil {
		http.Error(w, "JSON inválido", http.StatusBadRequest)
		return
	}

	datosSolicitud.DestinoX = strings.ToUpper(datosSolicitud.DestinoX)

	tx, err := conexionBaseDatos.Begin()
	if err != nil {
		http.Error(w, "Error en transacción", http.StatusInternalServerError)
		return
	}

	var estadoPartida string
	var turnoActual string
	err = tx.QueryRow("SELECT current_turn, status FROM games WHERE id = $1", gameID).Scan(&turnoActual, &estadoPartida)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Partida no encontrada", http.StatusBadRequest)
		return
	}

	if estadoPartida != "IN_PROGRESS" {
		tx.Rollback()
		http.Error(w, "La partida ya ha finalizado. Reiníciala para volver a jugar.", http.StatusBadRequest)
		return
	}

	var piezaActual Pieza
	err = tx.QueryRow("SELECT id, player, type, pos_x, pos_y FROM pieces WHERE id = $1 AND game_id = $2 AND is_alive = TRUE", datosSolicitud.IDPieza, gameID).
		Scan(&piezaActual.ID, &piezaActual.Jugador, &piezaActual.Tipo, &piezaActual.PosicionX, &piezaActual.PosicionY)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Pieza no encontrada", http.StatusBadRequest)
		return
	}

	if piezaActual.Jugador != turnoActual {
		tx.Rollback()
		http.Error(w, "Turno inválido", http.StatusBadRequest)
		return
	}

	var idPiezaDestino int
	var jugadorPiezaDestino string
	var tipoPiezaDestino string
	hayPiezaEnDestino := false
	err = tx.QueryRow("SELECT id, player, type FROM pieces WHERE game_id = $1 AND pos_x = $2 AND pos_y = $3 AND is_alive = TRUE", gameID, datosSolicitud.DestinoX, datosSolicitud.DestinoY).
		Scan(&idPiezaDestino, &jugadorPiezaDestino, &tipoPiezaDestino)
	if err == nil {
		hayPiezaEnDestino = true
		if jugadorPiezaDestino == piezaActual.Jugador {
			tx.Rollback()
			http.Error(w, "No puedes capturar tu propia pieza", http.StatusBadRequest)
			return
		}
	}

	if !esMovimientoValido(piezaActual, datosSolicitud.DestinoX, datosSolicitud.DestinoY, hayPiezaEnDestino, tx, gameID) {
		tx.Rollback()
		http.Error(w, "Movimiento no permitido", http.StatusBadRequest)
		return
	}

	reyCapturado := false
	if hayPiezaEnDestino {
		_, err = tx.Exec("UPDATE pieces SET is_alive = FALSE WHERE id = $1", idPiezaDestino)
		if err != nil {
			tx.Rollback()
			http.Error(w, "Error al eliminar pieza", http.StatusInternalServerError)
			return
		}
		if tipoPiezaDestino == "KING" {
			reyCapturado = true
		}
	}

	tipoFinalPieza := piezaActual.Tipo
	if piezaActual.Tipo == "PAWN" {
		if (piezaActual.Jugador == "WHITE" && datosSolicitud.DestinoY == 5) ||
			(piezaActual.Jugador == "BLACK" && datosSolicitud.DestinoY == 1) {
			tipoFinalPieza = "QUEEN"
		}
	}

	_, err = tx.Exec("UPDATE pieces SET pos_x = $1, pos_y = $2, type = $3 WHERE id = $4",
		datosSolicitud.DestinoX, datosSolicitud.DestinoY, tipoFinalPieza, datosSolicitud.IDPieza)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Error al mover pieza", http.StatusInternalServerError)
		return
	}

	promotedTo := sql.NullString{}
	if tipoFinalPieza == "QUEEN" && piezaActual.Tipo == "PAWN" {
		promotedTo.String = "QUEEN"
		promotedTo.Valid = true
	}

	_, err = tx.Exec("INSERT INTO movements (game_id, piece_id, from_x, from_y, to_x, to_y, promoted_to) VALUES ($1, $2, $3, $4, $5, $6, $7)",
		gameID, datosSolicitud.IDPieza, piezaActual.PosicionX, piezaActual.PosicionY, datosSolicitud.DestinoX, datosSolicitud.DestinoY, promotedTo)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Error al registrar movimiento", http.StatusInternalServerError)
		return
	}

	if reyCapturado {
		nuevoEstado := "WHITE_WON"
		if piezaActual.Jugador == "BLACK" {
			nuevoEstado = "BLACK_WON"
		}
		_, err = tx.Exec("UPDATE games SET status = $1 WHERE id = $2", nuevoEstado, gameID)
		if err != nil {
			tx.Rollback()
			http.Error(w, "Error al actualizar estado de victoria", http.StatusInternalServerError)
			return
		}
	} else {
		siguienteTurno := "WHITE"
		if turnoActual == "WHITE" {
			siguienteTurno = "BLACK"
		}
		_, err = tx.Exec("UPDATE games SET current_turn = $1 WHERE id = $2", siguienteTurno, gameID)
		if err != nil {
			tx.Rollback()
			http.Error(w, "Error al actualizar turno", http.StatusInternalServerError)
			return
		}
	}

	err = tx.Commit()
	if err != nil {
		http.Error(w, "Error al confirmar transacción", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"mensaje": "Movimiento exitoso"}`))
}

func reiniciarJuego(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	gameID := obtenerIDJuego(r)

	tx, err := conexionBaseDatos.Begin()
	if err != nil {
		http.Error(w, "Error al iniciar transacción", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec("DELETE FROM movements WHERE game_id = $1", gameID)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Error limpiando movimientos", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec("DELETE FROM pieces WHERE game_id = $1", gameID)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Error limpiando piezas", http.StatusInternalServerError)
		return
	}

	_, err = tx.Exec("UPDATE games SET current_turn = 'WHITE', status = 'IN_PROGRESS' WHERE id = $1", gameID)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Error reiniciando estado del juego", http.StatusInternalServerError)
		return
	}

	insertQuery := `
	INSERT INTO pieces (game_id, player, type, pos_x, pos_y) VALUES
	($1, 'WHITE', 'ROOK',   'A', 1), ($1, 'WHITE', 'BISHOP', 'B', 1), ($1, 'WHITE', 'KING',   'C', 1), ($1, 'WHITE', 'BISHOP', 'D', 1), ($1, 'WHITE', 'ROOK',   'E', 1),
	($1, 'WHITE', 'PAWN',   'A', 2), ($1, 'WHITE', 'PAWN',   'B', 2), ($1, 'WHITE', 'PAWN',   'C', 2), ($1, 'WHITE', 'PAWN',   'D', 2), ($1, 'WHITE', 'PAWN',   'E', 2),
	($1, 'BLACK', 'PAWN',   'A', 4), ($1, 'BLACK', 'PAWN',   'B', 4), ($1, 'BLACK', 'PAWN',   'C', 4), ($1, 'BLACK', 'PAWN',   'D', 4), ($1, 'BLACK', 'PAWN',   'E', 4),
	($1, 'BLACK', 'ROOK',   'A', 5), ($1, 'BLACK', 'BISHOP', 'B', 5), ($1, 'BLACK', 'KING',   'C', 5), ($1, 'BLACK', 'BISHOP', 'D', 5), ($1, 'BLACK', 'ROOK',   'E', 5)`

	_, err = tx.Exec(insertQuery, gameID)
	if err != nil {
		tx.Rollback()
		http.Error(w, "Error insertando piezas", http.StatusInternalServerError)
		return
	}

	err = tx.Commit()
	if err != nil {
		http.Error(w, "Error al confirmar transacción", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"mensaje": "Juego reiniciado exitosamente"}`))
}

func esMovimientoValido(pieza Pieza, destinoX string, destinoY int, hayCaptura bool, tx *sql.Tx, gameID int) bool {
	origenX := int(pieza.PosicionX[0] - 'A')
	origenY := pieza.PosicionY
	desX := int(destinoX[0] - 'A')
	desY := destinoY

	dx := desX - origenX
	dy := desY - origenY

	absDx := int(math.Abs(float64(dx)))
	absDy := int(math.Abs(float64(dy)))

	if dx == 0 && dy == 0 {
		return false
	}

	switch pieza.Tipo {
	case "PAWN":
		if pieza.Jugador == "WHITE" {
			if dx == 0 && dy == 1 && !hayCaptura {
				return true
			}
			if absDx == 1 && dy == 1 && hayCaptura {
				return true
			}
		} else {
			if dx == 0 && dy == -1 && !hayCaptura {
				return true
			}
			if absDx == 1 && dy == -1 && hayCaptura {
				return true
			}
		}
		return false

	case "ROOK":
		if dx != 0 && dy != 0 {
			return false
		}
		return !hayObstaculosEnCamino(origenX, origenY, desX, desY, tx, gameID)

	case "BISHOP":
		if absDx != absDy {
			return false
		}
		return !hayObstaculosEnCamino(origenX, origenY, desX, desY, tx, gameID)

	case "QUEEN":
		if (dx == 0 || dy == 0) || (absDx == absDy) {
			return !hayObstaculosEnCamino(origenX, origenY, desX, desY, tx, gameID)
		}
		return false

	case "KING":
		return absDx <= 1 && absDy <= 1
	}

	return false
}

func hayObstaculosEnCamino(origenX, origenY, desX, desY int, tx *sql.Tx, gameID int) bool {
	pasoX := 0
	if desX > origenX {
		pasoX = 1
	} else if desX < origenX {
		pasoX = -1
	}

	pasoY := 0
	if desY > origenY {
		pasoY = 1
	} else if desY < origenY {
		pasoY = -1
	}

	currX := origenX + pasoX
	currY := origenY + pasoY

	for currX != desX || currY != desY {
		colChar := string(rune('A' + currX))
		var existe int
		err := tx.QueryRow("SELECT id FROM pieces WHERE game_id = $1 AND pos_x = $2 AND pos_y = $3 AND is_alive = TRUE", gameID, colChar, currY).Scan(&existe)
		if err == nil {
			return true
		}
		currX += pasoX
		currY += pasoY
	}

	return false
}

func instanceMiddleware(siguiente http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Instance-Id", instanceID)
		siguiente(w, r)
	}
}

func logMiddleware(siguiente http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[Game Service %s] %s - %s", instanceID, r.Method, r.URL.Path)
		siguiente(w, r)
	}
}