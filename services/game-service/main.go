package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"strings"

	_ "github.com/lib/pq"
	pb "game-service/proto/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var conexionBaseDatos *sql.DB
var instanceID string

type gameServer struct {
	pb.UnimplementedGameServiceServer
}

func main() {
	instanceID = getEnv("INSTANCE_ID", "game-service-local")
	dbHost := getEnv("DB_HOST", "db")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "postgres")
	dbPassword := getEnv("DB_PASSWORD", "postgrespassword")
	dbName := getEnv("DB_NAME", "minichess")

	cadenaConexion := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost, dbPort, dbUser, dbPassword, dbName)

	var err error
	conexionBaseDatos, err = sql.Open("postgres", cadenaConexion)
	if err != nil {
		log.Fatalf("[Game Service %s] Error al abrir conexión con BD: %v", instanceID, err)
	}
	defer conexionBaseDatos.Close()

	if err = conexionBaseDatos.Ping(); err != nil {
		log.Printf("[Game Service %s] Advertencia BD no disponible de inmediato: %v", instanceID, err)
	}

	puerto := getEnv("PORT", "50053")
	listener, err := net.Listen("tcp", ":"+puerto)
	if err != nil {
		log.Fatalf("[Game Service %s] Error abriendo puerto TCP %s: %v", instanceID, puerto, err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterGameServiceServer(grpcServer, &gameServer{})

	log.Printf("[Game Service %s] Servidor gRPC corriendo en puerto :%s", instanceID, puerto)
	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("[Game Service %s] Error al iniciar servidor gRPC: %v", instanceID, err)
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

// --- IMPLEMENTACIÓN DE LOS MÉTODOS gRPC ---

func (s *gameServer) GetGameState(ctx context.Context, req *pb.GetGameRequest) (*pb.GameState, error) {
	gameID := req.GetGameId()
	if gameID == 0 {
		gameID = 1
	}

	var juegoActual pb.GameState
	juegoActual.InstanceId = instanceID

	err := conexionBaseDatos.QueryRowContext(ctx, "SELECT id, current_turn, status FROM games WHERE id = $1", gameID).
		Scan(&juegoActual.Id, &juegoActual.TurnoActual, &juegoActual.Estado)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "Partida %d no encontrada: %v", gameID, err)
	}

	filasObtenidas, err := conexionBaseDatos.QueryContext(ctx,
		"SELECT id, player, type, pos_x, pos_y, is_alive FROM pieces WHERE game_id = $1 AND is_alive = TRUE", gameID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error al consultar piezas: %v", err)
	}
	defer filasObtenidas.Close()

	for filasObtenidas.Next() {
		var p pb.Piece
		if errScan := filasObtenidas.Scan(&p.Id, &p.Jugador, &p.Tipo, &p.PosX, &p.PosY, &p.EstaViva); errScan != nil {
			continue
		}
		juegoActual.Piezas = append(juegoActual.Piezas, &p)
	}

	if errIter := filasObtenidas.Err(); errIter != nil {
		return nil, status.Errorf(codes.Internal, "Error durante la lectura de piezas: %v", errIter)
	}

	return &juegoActual, nil
}

func (s *gameServer) MakeMove(ctx context.Context, req *pb.MoveRequest) (*pb.GameState, error) {
	gameID := req.GetGameId()
	if gameID == 0 {
		gameID = 1
	}

	destinoX := strings.ToUpper(req.GetDestinoX())
	destinoY := req.GetDestinoY()

	// Validación de límites del tablero 5x5
	if len(destinoX) != 1 || destinoX[0] < 'A' || destinoX[0] > 'E' || destinoY < 1 || destinoY > 5 {
		return nil, status.Errorf(codes.InvalidArgument, "Movimiento fuera de los límites del tablero 5x5")
	}

	tx, err := conexionBaseDatos.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error en transacción BD")
	}

	var estadoPartida string
	var turnoActual string
	err = tx.QueryRowContext(ctx, "SELECT current_turn, status FROM games WHERE id = $1", gameID).Scan(&turnoActual, &estadoPartida)
	if err != nil {
		tx.Rollback()
		return nil, status.Errorf(codes.NotFound, "Partida no encontrada")
	}

	if estadoPartida != "IN_PROGRESS" {
		tx.Rollback()
		return nil, status.Errorf(codes.FailedPrecondition, "La partida ya ha finalizado")
	}

	type PiezaLocal struct {
		ID        int32
		Jugador   string
		Tipo      string
		PosicionX string
		PosicionY int32
	}

	var piezaActual PiezaLocal
	err = tx.QueryRowContext(ctx, "SELECT id, player, type, pos_x, pos_y FROM pieces WHERE id = $1 AND game_id = $2 AND is_alive = TRUE", req.GetIdPieza(), gameID).
		Scan(&piezaActual.ID, &piezaActual.Jugador, &piezaActual.Tipo, &piezaActual.PosicionX, &piezaActual.PosicionY)
	if err != nil {
		tx.Rollback()
		return nil, status.Errorf(codes.NotFound, "Pieza no encontrada o destruida")
	}

	if piezaActual.Jugador != turnoActual {
		tx.Rollback()
		return nil, status.Errorf(codes.PermissionDenied, "No es el turno de las %s", piezaActual.Jugador)
	}

	var idPiezaDestino int32
	var jugadorPiezaDestino string
	var tipoPiezaDestino string
	hayPiezaEnDestino := false
	err = tx.QueryRowContext(ctx, "SELECT id, player, type FROM pieces WHERE game_id = $1 AND pos_x = $2 AND pos_y = $3 AND is_alive = TRUE", gameID, destinoX, destinoY).
		Scan(&idPiezaDestino, &jugadorPiezaDestino, &tipoPiezaDestino)
	if err == nil {
		hayPiezaEnDestino = true
		if jugadorPiezaDestino == piezaActual.Jugador {
			tx.Rollback()
			return nil, status.Errorf(codes.InvalidArgument, "No puedes capturar tu propia pieza")
		}
	}

	if !esMovimientoValido(piezaActual.Tipo, piezaActual.Jugador, piezaActual.PosicionX, int(piezaActual.PosicionY), destinoX, int(destinoY), hayPiezaEnDestino, tx, int(gameID), ctx) {
		tx.Rollback()
		return nil, status.Errorf(codes.InvalidArgument, "Movimiento de %s no permitido", piezaActual.Tipo)
	}

	reyCapturado := false
	if hayPiezaEnDestino {
		_, err = tx.ExecContext(ctx, "UPDATE pieces SET is_alive = FALSE WHERE id = $1", idPiezaDestino)
		if err != nil {
			tx.Rollback()
			return nil, status.Errorf(codes.Internal, "Error al eliminar pieza destruida")
		}
		if tipoPiezaDestino == "KING" {
			reyCapturado = true
		}
	}

	tipoFinalPieza := piezaActual.Tipo
	if piezaActual.Tipo == "PAWN" {
		if (piezaActual.Jugador == "WHITE" && destinoY == 5) || (piezaActual.Jugador == "BLACK" && destinoY == 1) {
			tipoFinalPieza = "QUEEN"
		}
	}

	_, err = tx.ExecContext(ctx, "UPDATE pieces SET pos_x = $1, pos_y = $2, type = $3 WHERE id = $4",
		destinoX, destinoY, tipoFinalPieza, req.GetIdPieza())
	if err != nil {
		tx.Rollback()
		return nil, status.Errorf(codes.Internal, "Error al mover pieza")
	}

	if reyCapturado {
		nuevoEstado := "WHITE_WON"
		if piezaActual.Jugador == "BLACK" {
			nuevoEstado = "BLACK_WON"
		}
		_, err = tx.ExecContext(ctx, "UPDATE games SET status = $1 WHERE id = $2", nuevoEstado, gameID)
		if err != nil {
			tx.Rollback()
			return nil, status.Errorf(codes.Internal, "Error actualizando estado de victoria")
		}
	} else {
		siguienteTurno := "WHITE"
		if turnoActual == "WHITE" {
			siguienteTurno = "BLACK"
		}
		_, err = tx.ExecContext(ctx, "UPDATE games SET current_turn = $1 WHERE id = $2", siguienteTurno, gameID)
		if err != nil {
			tx.Rollback()
			return nil, status.Errorf(codes.Internal, "Error al actualizar turno")
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "Error al confirmar transacción")
	}

	return s.GetGameState(ctx, &pb.GetGameRequest{GameId: gameID})
}

func (s *gameServer) ResetGame(ctx context.Context, req *pb.GetGameRequest) (*pb.GameState, error) {
	gameID := req.GetGameId()
	if gameID == 0 {
		gameID = 1
	}

	tx, err := conexionBaseDatos.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error al iniciar transacción")
	}

	_, _ = tx.ExecContext(ctx, "DELETE FROM pieces WHERE game_id = $1", gameID)
	_, _ = tx.ExecContext(ctx, "UPDATE games SET current_turn = 'WHITE', status = 'IN_PROGRESS' WHERE id = $1", gameID)

	insertQuery := `
	INSERT INTO pieces (game_id, player, type, pos_x, pos_y, is_alive) VALUES
	($1, 'WHITE', 'ROOK',   'A', 1, true), ($1, 'WHITE', 'BISHOP', 'B', 1, true), ($1, 'WHITE', 'KING',   'C', 1, true), ($1, 'WHITE', 'BISHOP', 'D', 1, true), ($1, 'WHITE', 'ROOK',   'E', 1, true),
	($1, 'WHITE', 'PAWN',   'A', 2, true), ($1, 'WHITE', 'PAWN',   'B', 2, true), ($1, 'WHITE', 'PAWN',   'C', 2, true), ($1, 'WHITE', 'PAWN',   'D', 2, true), ($1, 'WHITE', 'PAWN',   'E', 2, true),
	($1, 'BLACK', 'PAWN',   'A', 4, true), ($1, 'BLACK', 'PAWN',   'B', 4, true), ($1, 'BLACK', 'PAWN',   'C', 4, true), ($1, 'BLACK', 'PAWN',   'D', 4, true), ($1, 'BLACK', 'PAWN',   'E', 4, true),
	($1, 'BLACK', 'ROOK',   'A', 5, true), ($1, 'BLACK', 'BISHOP', 'B', 5, true), ($1, 'BLACK', 'KING',   'C', 5, true), ($1, 'BLACK', 'BISHOP', 'D', 5, true), ($1, 'BLACK', 'ROOK',   'E', 5, true)`

	_, err = tx.ExecContext(ctx, insertQuery, gameID)
	if err != nil {
		tx.Rollback()
		return nil, status.Errorf(codes.Internal, "Error reinsertando piezas")
	}

	if err = tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "Error al confirmar reinicio")
	}

	return s.GetGameState(ctx, req)
}

// --- REGLAS DE MOVIMIENTO EN EL TABLERO 5x5 ---

func esMovimientoValido(tipo, jugador, posX string, posY int, destinoX string, destinoY int, hayCaptura bool, tx *sql.Tx, gameID int, ctx context.Context) bool {
	origenX := int(posX[0] - 'A')
	origenY := posY
	desX := int(destinoX[0] - 'A')
	desY := destinoY

	dx := desX - origenX
	dy := desY - origenY

	absDx := int(math.Abs(float64(dx)))
	absDy := int(math.Abs(float64(dy)))

	if dx == 0 && dy == 0 {
		return false
	}

	switch tipo {
	case "PAWN":
		if jugador == "WHITE" {
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
		return !hayObstaculosEnCamino(origenX, origenY, desX, desY, tx, gameID, ctx)

	case "BISHOP":
		if absDx != absDy {
			return false
		}
		return !hayObstaculosEnCamino(origenX, origenY, desX, desY, tx, gameID, ctx)

	case "QUEEN":
		if (dx == 0 || dy == 0) || (absDx == absDy) {
			return !hayObstaculosEnCamino(origenX, origenY, desX, desY, tx, gameID, ctx)
		}
		return false

	case "KING":
		return absDx <= 1 && absDy <= 1
	}

	return false
}

func hayObstaculosEnCamino(origenX, origenY, desX, desY int, tx *sql.Tx, gameID int, ctx context.Context) bool {
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
		err := tx.QueryRowContext(ctx, "SELECT id FROM pieces WHERE game_id = $1 AND pos_x = $2 AND pos_y = $3 AND is_alive = TRUE", gameID, colChar, currY).Scan(&existe)
		if err == nil {
			return true
		}
		currX += pasoX
		currY += pasoY
	}

	return false
}
