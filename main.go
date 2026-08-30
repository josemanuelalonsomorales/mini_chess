package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"

	_ "github.com/lib/pq"
)

var conexionBaseDatos *sql.DB

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
	var err error
	cadenaConexion := "user=postgres password=sh0ut1nk dbname=transferencias_db sslmode=disable"

	conexionBaseDatos, err = sql.Open("postgres", cadenaConexion)
	if err != nil {
		log.Fatal(err)
	}

	// Archivos estáticos
	http.Handle("/", http.FileServer(http.Dir("./public")))

	// Endpoints con Middleware aplicado
	http.Handle("/api/juego", logMiddleware(http.HandlerFunc(getEstadoJuego)))
	http.Handle("/api/mover", logMiddleware(http.HandlerFunc(hacerMovimiento)))
	http.Handle("/api/reiniciar", logMiddleware(http.HandlerFunc(reiniciarJuego)))

	fmt.Println("Servidor corriendo exitosamente en http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func getEstadoJuego(respuestahttp http.ResponseWriter, peticionhttp *http.Request) {
	var juegoActual Juego
	err := conexionBaseDatos.QueryRow("SELECT id, current_turn, status FROM games WHERE id = $1", 1).
		Scan(&juegoActual.ID, &juegoActual.TurnoActual, &juegoActual.Estado)
	if err != nil {
		http.Error(respuestahttp, "Error al consultar juego", 500)
		return
	}

	var filasObtenidas *sql.Rows
	filasObtenidas, err = conexionBaseDatos.Query("SELECT id, game_id, player, type, pos_x, pos_y, is_alive FROM pieces WHERE game_id = $1 AND is_alive = TRUE", 1)
	if err != nil {
		http.Error(respuestahttp, "Error al consultar piezas", 500)
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

	respuestahttp.Header().Set("Content-Type", "application/json")
	json.NewEncoder(respuestahttp).Encode(juegoActual)
}

func hacerMovimiento(respuestahttp http.ResponseWriter, peticionhttp *http.Request) {
	if peticionhttp.Method != "POST" {
		http.Error(respuestahttp, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	var datosSolicitud SolicitudMovimiento
	err := json.NewDecoder(peticionhttp.Body).Decode(&datosSolicitud)
	if err != nil {
		http.Error(respuestahttp, "JSON inválido", 400)
		return
	}

	datosSolicitud.DestinoX = strings.ToUpper(datosSolicitud.DestinoX)

	transaccionBD, err := conexionBaseDatos.Begin()
	if err != nil {
		http.Error(respuestahttp, "Error en transaccion", 500)
		return
	}

	var estadoPartida string
	var turnoActual string
	err = transaccionBD.QueryRow("SELECT current_turn, status FROM games WHERE id = $1", 1).Scan(&turnoActual, &estadoPartida)
	if err != nil {
		transaccionBD.Rollback()
		http.Error(respuestahttp, "Partida no encontrada", 400)
		return
	}

	if estadoPartida != "IN_PROGRESS" {
		transaccionBD.Rollback()
		http.Error(respuestahttp, "La partida ya ha finalizado. Reiníciala para volver a jugar.", 400)
		return
	}

	var piezaActual Pieza
	err = transaccionBD.QueryRow("SELECT id, player, type, pos_x, pos_y FROM pieces WHERE id = $1 AND game_id = $2 AND is_alive = TRUE", datosSolicitud.IDPieza, 1).
		Scan(&piezaActual.ID, &piezaActual.Jugador, &piezaActual.Tipo, &piezaActual.PosicionX, &piezaActual.PosicionY)
	if err != nil {
		transaccionBD.Rollback()
		http.Error(respuestahttp, "Pieza no encontrada", 400)
		return
	}

	if piezaActual.Jugador != turnoActual {
		transaccionBD.Rollback()
		http.Error(respuestahttp, "Turno invalido", 400)
		return
	}

	var idPiezaDestino int
	var jugadorPiezaDestino string
	var tipoPiezaDestino string
	hayPiezaEnDestino := false
	err = transaccionBD.QueryRow("SELECT id, player, type FROM pieces WHERE game_id = $1 AND pos_x = $2 AND pos_y = $3 AND is_alive = TRUE", 1, datosSolicitud.DestinoX, datosSolicitud.DestinoY).
		Scan(&idPiezaDestino, &jugadorPiezaDestino, &tipoPiezaDestino)
	if err == nil {
		hayPiezaEnDestino = true
		if jugadorPiezaDestino == piezaActual.Jugador {
			transaccionBD.Rollback()
			http.Error(respuestahttp, "No puedes capturar tu propia pieza", 400)
			return
		}
	}

	if !esMovimientoValido(piezaActual, datosSolicitud.DestinoX, datosSolicitud.DestinoY, hayPiezaEnDestino, transaccionBD) {
		transaccionBD.Rollback()
		http.Error(respuestahttp, "Movimiento no permitido para esta pieza", 400)
		return
	}

	reyCapturado := false
	if hayPiezaEnDestino {
		_, err = transaccionBD.Exec("UPDATE pieces SET is_alive = FALSE WHERE id = $1", idPiezaDestino)
		if err != nil {
			transaccionBD.Rollback()
			http.Error(respuestahttp, "Error al eliminar pieza", 400)
			return
		}
		if tipoPiezaDestino == "KING" {
			reyCapturado = true
		}
	}

	_, err = transaccionBD.Exec("UPDATE pieces SET pos_x = $1, pos_y = $2 WHERE id = $3", datosSolicitud.DestinoX, datosSolicitud.DestinoY, datosSolicitud.IDPieza)
	if err != nil {
		transaccionBD.Rollback()
		http.Error(respuestahttp, "Error al mover pieza", 400)
		return
	}

	_, err = transaccionBD.Exec("INSERT INTO movements (game_id, piece_id, from_x, from_y, to_x, to_y) VALUES ($1, $2, $3, $4, $5, $6)",
		1, datosSolicitud.IDPieza, piezaActual.PosicionX, piezaActual.PosicionY, datosSolicitud.DestinoX, datosSolicitud.DestinoY)
	if err != nil {
		transaccionBD.Rollback()
		http.Error(respuestahttp, "Error al registrar movimiento", 400)
		return
	}

	if reyCapturado {
		nuevoEstado := "WHITE_WON"
		if piezaActual.Jugador == "BLACK" {
			nuevoEstado = "BLACK_WON"
		}
		_, err = transaccionBD.Exec("UPDATE games SET status = $1 WHERE id = $2", nuevoEstado, 1)
		if err != nil {
			transaccionBD.Rollback()
			http.Error(respuestahttp, "Error al actualizar estado de victoria", 400)
			return
		}
	} else {
		siguienteTurno := "WHITE"
		if turnoActual == "WHITE" {
			siguienteTurno = "BLACK"
		}
		_, err = transaccionBD.Exec("UPDATE games SET current_turn = $1 WHERE id = $2", siguienteTurno, 1)
		if err != nil {
			transaccionBD.Rollback()
			http.Error(respuestahttp, "Error al actualizar turno", 400)
			return
		}
	}

	err = transaccionBD.Commit()
	if err != nil {
		http.Error(respuestahttp, "Error al confirmar transaccion", 500)
		return
	}

	respuestahttp.Header().Set("Content-Type", "application/json")
	respuestahttp.WriteHeader(http.StatusOK)
	respuestahttp.Write([]byte(`{"mensaje": "Movimiento exitoso"}`))
}

func reiniciarJuego(respuestahttp http.ResponseWriter, peticionhttp *http.Request) {
	if peticionhttp.Method != "POST" {
		http.Error(respuestahttp, "Método no permitido", http.StatusMethodNotAllowed)
		return
	}

	tx, err := conexionBaseDatos.Begin()
	if err != nil {
		http.Error(respuestahttp, "Error al iniciar transaccion", 500)
		return
	}

	_, err = tx.Exec("DELETE FROM movements WHERE game_id = 1")
	if err != nil {
		tx.Rollback()
		http.Error(respuestahttp, "Error limpiando movimientos", 500)
		return
	}

	_, err = tx.Exec("DELETE FROM pieces WHERE game_id = 1")
	if err != nil {
		tx.Rollback()
		http.Error(respuestahttp, "Error limpiando piezas", 500)
		return
	}

	_, err = tx.Exec("UPDATE games SET current_turn = 'WHITE', status = 'IN_PROGRESS' WHERE id = 1")
	if err != nil {
		tx.Rollback()
		http.Error(respuestahttp, "Error reiniciando estado del juego", 500)
		return
	}

	insertQuery := `
	INSERT INTO pieces (game_id, player, type, pos_x, pos_y) VALUES
	(1, 'WHITE', 'ROOK',   'A', 1), (1, 'WHITE', 'BISHOP', 'B', 1), (1, 'WHITE', 'KING',   'C', 1), (1, 'WHITE', 'BISHOP', 'D', 1), (1, 'WHITE', 'ROOK',   'E', 1),
	(1, 'WHITE', 'PAWN',   'A', 2), (1, 'WHITE', 'PAWN',   'B', 2), (1, 'WHITE', 'PAWN',   'C', 2), (1, 'WHITE', 'PAWN',   'D', 2), (1, 'WHITE', 'PAWN',   'E', 2),
	(1, 'BLACK', 'PAWN',   'A', 4), (1, 'BLACK', 'PAWN',   'B', 4), (1, 'BLACK', 'PAWN',   'C', 4), (1, 'BLACK', 'PAWN',   'D', 4), (1, 'BLACK', 'PAWN',   'E', 4),
	(1, 'BLACK', 'ROOK',   'A', 5), (1, 'BLACK', 'BISHOP', 'B', 5), (1, 'BLACK', 'KING',   'C', 5), (1, 'BLACK', 'BISHOP', 'D', 5), (1, 'BLACK', 'ROOK',   'E', 5)`

	_, err = tx.Exec(insertQuery)
	if err != nil {
		tx.Rollback()
		http.Error(respuestahttp, "Error insertando piezas", 500)
		return
	}

	err = tx.Commit()
	if err != nil {
		http.Error(respuestahttp, "Error al confirmar transaccion", 500)
		return
	}

	respuestahttp.Header().Set("Content-Type", "application/json")
	respuestahttp.WriteHeader(http.StatusOK)
	respuestahttp.Write([]byte(`{"mensaje": "Juego reiniciado exitosamente"}`))
}

func esMovimientoValido(pieza Pieza, destinoX string, destinoY int, hayCaptura bool, tx *sql.Tx) bool {
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
		return !hayObstaculosEnCamino(origenX, origenY, desX, desY, tx)

	case "BISHOP":
		if absDx != absDy {
			return false
		}
		return !hayObstaculosEnCamino(origenX, origenY, desX, desY, tx)

	case "KING":
		return absDx <= 1 && absDy <= 1
	}

	return false
}

func hayObstaculosEnCamino(origenX, origenY, desX, desY int, tx *sql.Tx) bool {
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
		err := tx.QueryRow("SELECT id FROM pieces WHERE game_id = 1 AND pos_x = $1 AND pos_y = $2 AND is_alive = TRUE", colChar, currY).Scan(&existe)
		if err == nil {
			return true
		}
		currX += pasoX
		currY += pasoY
	}

	return false
}

func logMiddleware(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[PETICIÓN] %s - %s", r.Method, r.URL.Path)
		siguiente.ServeHTTP(w, r)
	})
}
