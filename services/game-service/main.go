package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
	pb "mini-chess/proto/pb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var conexionBaseDatos *sql.DB
var instanceID string

const (
	tableroMinX = 'A'
	tableroMaxX = 'E'
	tableroMinY = 1
	tableroMaxY = 5
)

type gameServer struct {
	pb.UnimplementedGameServiceServer
	publisher *gameKafkaPublisher
}

type PiezaLocal struct {
	ID        int32
	Jugador   string
	Tipo      string
	PosicionX string
	PosicionY int32
}

func main() {
	instanceID = getEnv("INSTANCE_ID", "game-service-local")

	dbHost := getEnv("DB_HOST", "db")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "postgres")
	dbPassword := getEnv("DB_PASSWORD", "postgrespassword")
	dbName := getEnv("DB_NAME", "minichess")

	cadenaConexion := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		dbHost,
		dbPort,
		dbUser,
		dbPassword,
		dbName,
	)

	var err error
	conexionBaseDatos, err = sql.Open("postgres", cadenaConexion)
	if err != nil {
		log.Fatalf("[Game Service %s] Error al abrir conexión con BD: %v", instanceID, err)
	}
	defer conexionBaseDatos.Close()

	conexionBaseDatos.SetMaxOpenConns(20)
	conexionBaseDatos.SetMaxIdleConns(10)
	conexionBaseDatos.SetConnMaxLifetime(30 * time.Minute)

	if err := esperarBaseDatos(30, 2*time.Second); err != nil {
		log.Fatalf("[Game Service %s] No fue posible conectar con PostgreSQL: %v", instanceID, err)
	}

	puerto := getEnv("PORT", "50053")

	listener, err := net.Listen("tcp", ":"+puerto)
	if err != nil {
		log.Fatalf("[Game Service %s] Error abriendo puerto TCP %s: %v", instanceID, puerto, err)
	}

	grpcServer := grpc.NewServer()

	publisher := newGameKafkaPublisher()
	defer publisher.close()

	pb.RegisterGameServiceServer(grpcServer, &gameServer{publisher: publisher})

	log.Printf("[Game Service %s] Servidor gRPC corriendo en puerto :%s", instanceID, puerto)

	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("[Game Service %s] Error al iniciar servidor gRPC: %v", instanceID, err)
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists && strings.TrimSpace(value) != "" {
		return value
	}

	return defaultValue
}

// esperarBaseDatos verifica la disponibilidad de PostgreSQL antes de aceptar solicitudes gRPC.
func esperarBaseDatos(maxIntentos int, intervalo time.Duration) error {
	var ultimoError error

	for intento := 1; intento <= maxIntentos; intento++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := conexionBaseDatos.PingContext(ctx)
		cancel()

		if err == nil {
			log.Printf("[Game Service %s] Conexión con PostgreSQL establecida", instanceID)
			return nil
		}

		ultimoError = err

		log.Printf(
			"[Game Service %s] Intento %d/%d de conexión a PostgreSQL fallido: %v",
			instanceID,
			intento,
			maxIntentos,
			err,
		)

		if intento < maxIntentos {
			time.Sleep(intervalo)
		}
	}

	return fmt.Errorf("PostgreSQL no disponible después de %d intentos: %w", maxIntentos, ultimoError)
}

func normalizarGameID(gameID int32) int32 {
	if gameID == 0 {
		return 1
	}

	return gameID
}

func validarGameID(gameID int32) error {
	if gameID <= 0 {
		return status.Error(codes.InvalidArgument, "Identificador de partida inválido")
	}

	return nil
}

func convertirErrorSQL(err error, recurso string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return status.Errorf(codes.NotFound, "%s no encontrado", recurso)
	}

	return status.Errorf(codes.Internal, "Error de base de datos al consultar %s: %v", recurso, err)
}

// GetGameState devuelve el estado completo de una partida y todas sus piezas vivas.
func (s *gameServer) GetGameState(
	ctx context.Context,
	req *pb.GetGameRequest,
) (*pb.GameState, error) {
	gameID := normalizarGameID(req.GetGameId())

	if err := validarGameID(gameID); err != nil {
		return nil, err
	}

	var juegoActual pb.GameState
	juegoActual.InstanceId = instanceID

	err := conexionBaseDatos.QueryRowContext(
		ctx,
		"SELECT id, current_turn, status FROM games WHERE id = $1",
		gameID,
	).Scan(
		&juegoActual.Id,
		&juegoActual.TurnoActual,
		&juegoActual.Estado,
	)

	if err != nil {
		return nil, convertirErrorSQL(err, fmt.Sprintf("partida %d", gameID))
	}

	filasObtenidas, err := conexionBaseDatos.QueryContext(
		ctx,
		`SELECT id, player, type, pos_x, pos_y, is_alive
		 FROM pieces
		 WHERE game_id = $1 AND is_alive = TRUE
		 ORDER BY id`,
		gameID,
	)

	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error al consultar piezas: %v", err)
	}
	defer filasObtenidas.Close()

	juegoActual.Piezas = make([]*pb.Piece, 0)

	for filasObtenidas.Next() {
		var pieza pb.Piece

		if err := filasObtenidas.Scan(
			&pieza.Id,
			&pieza.Jugador,
			&pieza.Tipo,
			&pieza.PosX,
			&pieza.PosY,
			&pieza.EstaViva,
		); err != nil {
			return nil, status.Errorf(codes.Internal, "Error al leer una pieza: %v", err)
		}

		juegoActual.Piezas = append(juegoActual.Piezas, &pieza)
	}

	if err := filasObtenidas.Err(); err != nil {
		return nil, status.Errorf(codes.Internal, "Error durante la lectura de piezas: %v", err)
	}

	return &juegoActual, nil
}

// MakeMove ejecuta un movimiento completo dentro de una transacción.
func (s *gameServer) MakeMove(
	ctx context.Context,
	req *pb.MoveRequest,
) (*pb.GameState, error) {
	gameID := normalizarGameID(req.GetGameId())

	if err := validarGameID(gameID); err != nil {
		return nil, err
	}

	if req.GetIdPieza() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "Identificador de pieza inválido")
	}

	destinoX := strings.ToUpper(strings.TrimSpace(req.GetDestinoX()))
	destinoY := req.GetDestinoY()

	if len(destinoX) != 1 ||
		destinoX[0] < tableroMinX ||
		destinoX[0] > tableroMaxX ||
		destinoY < tableroMinY ||
		destinoY > tableroMaxY {
		return nil, status.Error(codes.InvalidArgument, "Movimiento fuera de los límites del tablero 5x5")
	}

	tx, err := conexionBaseDatos.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error al iniciar transacción: %v", err)
	}
	defer tx.Rollback()

	// El bloqueo de la partida serializa los movimientos entre todas las instancias del servicio.
	var estadoPartida string
	var turnoActual string

	err = tx.QueryRowContext(
		ctx,
		`SELECT current_turn, status
		 FROM games
		 WHERE id = $1
		 FOR UPDATE`,
		gameID,
	).Scan(&turnoActual, &estadoPartida)

	if err != nil {
		return nil, convertirErrorSQL(err, fmt.Sprintf("partida %d", gameID))
	}

	if estadoPartida != "IN_PROGRESS" {
		return nil, status.Error(codes.FailedPrecondition, "La partida ya ha finalizado")
	}

	var piezaActual PiezaLocal

	err = tx.QueryRowContext(
		ctx,
		`SELECT id, player, type, pos_x, pos_y
		 FROM pieces
		 WHERE id = $1 AND game_id = $2 AND is_alive = TRUE
		 FOR UPDATE`,
		req.GetIdPieza(),
		gameID,
	).Scan(
		&piezaActual.ID,
		&piezaActual.Jugador,
		&piezaActual.Tipo,
		&piezaActual.PosicionX,
		&piezaActual.PosicionY,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "Pieza no encontrada o destruida")
		}

		return nil, status.Errorf(codes.Internal, "Error al consultar pieza: %v", err)
	}

	if piezaActual.Jugador != turnoActual {
		return nil, status.Errorf(
			codes.PermissionDenied,
			"No es el turno de las %s",
			piezaActual.Jugador,
		)
	}

	if !posicionValida(piezaActual.PosicionX, piezaActual.PosicionY) {
		return nil, status.Error(codes.Internal, "La posición actual de la pieza es inválida")
	}

	var idPiezaDestino int32
	var jugadorPiezaDestino string
	var tipoPiezaDestino string

	hayPiezaEnDestino := false

	err = tx.QueryRowContext(
		ctx,
		`SELECT id, player, type
		 FROM pieces
		 WHERE game_id = $1
		   AND pos_x = $2
		   AND pos_y = $3
		   AND is_alive = TRUE`,
		gameID,
		destinoX,
		destinoY,
	).Scan(
		&idPiezaDestino,
		&jugadorPiezaDestino,
		&tipoPiezaDestino,
	)

	if err == nil {
		hayPiezaEnDestino = true

		if jugadorPiezaDestino == piezaActual.Jugador {
			return nil, status.Error(codes.InvalidArgument, "No puedes capturar tu propia pieza")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.Internal, "Error al consultar pieza de destino: %v", err)
	}

	movimientoValido, err := esMovimientoValido(
		piezaActual.Tipo,
		piezaActual.Jugador,
		piezaActual.PosicionX,
		int(piezaActual.PosicionY),
		destinoX,
		int(destinoY),
		hayPiezaEnDestino,
		tx,
		int(gameID),
		ctx,
	)

	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error al comprobar el movimiento: %v", err)
	}

	if !movimientoValido {
		return nil, status.Errorf(codes.InvalidArgument, "Movimiento de %s no permitido", piezaActual.Tipo)
	}

	tipoFinalPieza := piezaActual.Tipo
	promocion := ""

	if piezaActual.Tipo == "PAWN" {
		if (piezaActual.Jugador == "WHITE" && destinoY == 5) ||
			(piezaActual.Jugador == "BLACK" && destinoY == 1) {
			tipoFinalPieza = "QUEEN"
			promocion = "QUEEN"
		}
	}

	// El registro del movimiento comparte la transacción con la captura y el desplazamiento.
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO movements
			(game_id, player_id, piece_id, from_x, from_y, to_x, to_y, promoted_to)
		 VALUES ($1, NULL, $2, $3, $4, $5, $6, NULLIF($7, ''))`,
		gameID,
		piezaActual.ID,
		piezaActual.PosicionX,
		piezaActual.PosicionY,
		destinoX,
		destinoY,
		promocion,
	)

	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error al registrar movimiento: %v", err)
	}

	reyCapturado := false

	if hayPiezaEnDestino {
		resultado, err := tx.ExecContext(
			ctx,
			`UPDATE pieces
			 SET is_alive = FALSE
			 WHERE id = $1 AND game_id = $2 AND is_alive = TRUE`,
			idPiezaDestino,
			gameID,
		)

		if err != nil {
			return nil, status.Errorf(codes.Internal, "Error al capturar pieza: %v", err)
		}

		filasAfectadas, err := resultado.RowsAffected()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Error verificando captura: %v", err)
		}

		if filasAfectadas != 1 {
			return nil, status.Error(codes.Aborted, "La pieza de destino cambió durante el movimiento")
		}

		reyCapturado = tipoPiezaDestino == "KING"
	}

	resultado, err := tx.ExecContext(
		ctx,
		`UPDATE pieces
		 SET pos_x = $1, pos_y = $2, type = $3
		 WHERE id = $4 AND game_id = $5 AND is_alive = TRUE`,
		destinoX,
		destinoY,
		tipoFinalPieza,
		piezaActual.ID,
		gameID,
	)

	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error al mover pieza: %v", err)
	}

	filasAfectadas, err := resultado.RowsAffected()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error verificando desplazamiento: %v", err)
	}

	if filasAfectadas != 1 {
		return nil, status.Error(codes.Aborted, "No se pudo actualizar la pieza")
	}

	if reyCapturado {
		nuevoEstado := "WHITE_WON"

		if piezaActual.Jugador == "BLACK" {
			nuevoEstado = "BLACK_WON"
		}

		_, err = tx.ExecContext(
			ctx,
			"UPDATE games SET status = $1 WHERE id = $2",
			nuevoEstado,
			gameID,
		)
	} else {
		siguienteTurno := "WHITE"

		if turnoActual == "WHITE" {
			siguienteTurno = "BLACK"
		}

		_, err = tx.ExecContext(
			ctx,
			"UPDATE games SET current_turn = $1 WHERE id = $2",
			siguienteTurno,
			gameID,
		)
	}

	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error al actualizar el estado de la partida: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "Error al confirmar movimiento: %v", err)
	}

	s.publisher.publish(
		"game.move_completed",
		gameID,
		fmt.Sprintf(
			"piece_id=%d,from=%s%d,to=%s%d,promotion=%s",
			piezaActual.ID,
			piezaActual.PosicionX,
			piezaActual.PosicionY,
			destinoX,
			destinoY,
			promocion,
		),
	)

	return s.GetGameState(ctx, &pb.GetGameRequest{GameId: gameID})
}

// ResetGame restaura las piezas y el turno inicial dentro de una sola transacción.
func (s *gameServer) ResetGame(
	ctx context.Context,
	req *pb.GetGameRequest,
) (*pb.GameState, error) {
	gameID := normalizarGameID(req.GetGameId())

	if err := validarGameID(gameID); err != nil {
		return nil, err
	}

	tx, err := conexionBaseDatos.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error al iniciar transacción: %v", err)
	}
	defer tx.Rollback()

	// El bloqueo impide que un movimiento concurrente se mezcle con el reinicio.
	var estadoActual string

	err = tx.QueryRowContext(
		ctx,
		"SELECT status FROM games WHERE id = $1 FOR UPDATE",
		gameID,
	).Scan(&estadoActual)

	if err != nil {
		return nil, convertirErrorSQL(err, fmt.Sprintf("partida %d", gameID))
	}

	_, err = tx.ExecContext(
		ctx,
		"DELETE FROM pieces WHERE game_id = $1",
		gameID,
	)

	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error eliminando piezas anteriores: %v", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE games
		 SET current_turn = 'WHITE', status = 'IN_PROGRESS', winner_id = NULL
		 WHERE id = $1`,
		gameID,
	)

	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error actualizando partida: %v", err)
	}

	insertQuery := `
	INSERT INTO pieces (game_id, player, type, pos_x, pos_y, is_alive) VALUES
	($1, 'WHITE', 'ROOK',   'A', 1, TRUE),
	($1, 'WHITE', 'BISHOP', 'B', 1, TRUE),
	($1, 'WHITE', 'KING',   'C', 1, TRUE),
	($1, 'WHITE', 'BISHOP', 'D', 1, TRUE),
	($1, 'WHITE', 'ROOK',   'E', 1, TRUE),
	($1, 'WHITE', 'PAWN',   'A', 2, TRUE),
	($1, 'WHITE', 'PAWN',   'B', 2, TRUE),
	($1, 'WHITE', 'PAWN',   'C', 2, TRUE),
	($1, 'WHITE', 'PAWN',   'D', 2, TRUE),
	($1, 'WHITE', 'PAWN',   'E', 2, TRUE),
	($1, 'BLACK', 'PAWN',   'A', 4, TRUE),
	($1, 'BLACK', 'PAWN',   'B', 4, TRUE),
	($1, 'BLACK', 'PAWN',   'C', 4, TRUE),
	($1, 'BLACK', 'PAWN',   'D', 4, TRUE),
	($1, 'BLACK', 'PAWN',   'E', 4, TRUE),
	($1, 'BLACK', 'ROOK',   'A', 5, TRUE),
	($1, 'BLACK', 'BISHOP', 'B', 5, TRUE),
	($1, 'BLACK', 'KING',   'C', 5, TRUE),
	($1, 'BLACK', 'BISHOP', 'D', 5, TRUE),
	($1, 'BLACK', 'ROOK',   'E', 5, TRUE)`

	_, err = tx.ExecContext(ctx, insertQuery, gameID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Error reinsertando piezas: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "Error al confirmar reinicio: %v", err)
	}

	s.publisher.publish(
		"game.reset",
		gameID,
		"board_restored_to_initial_position",
	)

	return s.GetGameState(ctx, &pb.GetGameRequest{GameId: gameID})
}

func posicionValida(posX string, posY int32) bool {
	return len(posX) == 1 &&
		posX[0] >= tableroMinX &&
		posX[0] <= tableroMaxX &&
		posY >= tableroMinY &&
		posY <= tableroMaxY
}

// esMovimientoValido comprueba las reglas de desplazamiento de cada tipo de pieza.
func esMovimientoValido(
	tipo string,
	jugador string,
	posX string,
	posY int,
	destinoX string,
	destinoY int,
	hayCaptura bool,
	tx *sql.Tx,
	gameID int,
	ctx context.Context,
) (bool, error) {
	if len(posX) != 1 || len(destinoX) != 1 {
		return false, nil
	}

	origenX := int(posX[0] - 'A')
	origenY := posY
	desX := int(destinoX[0] - 'A')
	desY := destinoY

	dx := desX - origenX
	dy := desY - origenY

	absDx := int(math.Abs(float64(dx)))
	absDy := int(math.Abs(float64(dy)))

	if dx == 0 && dy == 0 {
		return false, nil
	}

	switch tipo {
	case "PAWN":
		if jugador == "WHITE" {
			if dx == 0 && dy == 1 && !hayCaptura {
				return true, nil
			}

			if absDx == 1 && dy == 1 && hayCaptura {
				return true, nil
			}
		} else if jugador == "BLACK" {
			if dx == 0 && dy == -1 && !hayCaptura {
				return true, nil
			}

			if absDx == 1 && dy == -1 && hayCaptura {
				return true, nil
			}
		}

		return false, nil

	case "ROOK":
		if dx != 0 && dy != 0 {
			return false, nil
		}

		obstaculo, err := hayObstaculosEnCamino(
			origenX, origenY, desX, desY, tx, gameID, ctx,
		)

		return !obstaculo, err

	case "BISHOP":
		if absDx != absDy {
			return false, nil
		}

		obstaculo, err := hayObstaculosEnCamino(
			origenX, origenY, desX, desY, tx, gameID, ctx,
		)

		return !obstaculo, err

	case "QUEEN":
		if dx != 0 && dy != 0 && absDx != absDy {
			return false, nil
		}

		obstaculo, err := hayObstaculosEnCamino(
			origenX, origenY, desX, desY, tx, gameID, ctx,
		)

		return !obstaculo, err

	case "KING":
		return absDx <= 1 && absDy <= 1, nil
	}

	return false, nil
}

// hayObstaculosEnCamino consulta las casillas intermedias y propaga cualquier error SQL.
func hayObstaculosEnCamino(
	origenX int,
	origenY int,
	desX int,
	desY int,
	tx *sql.Tx,
	gameID int,
	ctx context.Context,
) (bool, error) {
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
		columna := string(rune('A' + currX))

		var existe bool

		err := tx.QueryRowContext(
			ctx,
			`SELECT EXISTS (
				SELECT 1
				FROM pieces
				WHERE game_id = $1
				  AND pos_x = $2
				  AND pos_y = $3
				  AND is_alive = TRUE
			)`,
			gameID,
			columna,
			currY,
		).Scan(&existe)

		if err != nil {
			return false, err
		}

		if existe {
			return true, nil
		}

		currX += pasoX
		currY += pasoY
	}

	return false, nil
}
