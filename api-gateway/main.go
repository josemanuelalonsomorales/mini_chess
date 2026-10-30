package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	pb "mini-chess/proto/pb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	defaultPort           = "8001"
	defaultGameID         = 1
	defaultRequestTimeout = 5 * time.Second
	healthCheckInterval   = 5 * time.Second
	healthCheckTimeout    = 3 * time.Second
	maxRequestBodySize    = 1 << 20 // 1 MiB
)

type Backend struct {
	Address    string
	InstanceID string
	Connection *grpc.ClientConn
	Client     pb.GameServiceClient

	mu      sync.RWMutex
	healthy bool
}

func (b *Backend) IsHealthy() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.healthy
}

func (b *Backend) SetHealthy(value bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.healthy = value
}

type Gateway struct {
	backends        []*Backend
	counter         atomic.Uint64
	gameID          int32
	usersConnection *grpc.ClientConn
	usersClient     pb.UserServiceClient
	jwtSecret       []byte
}

type PieceResponse struct {
	ID       int32  `json:"id"`
	Jugador  string `json:"jugador"`
	Tipo     string `json:"tipo"`
	PosX     string `json:"pos_x"`
	PosY     int32  `json:"pos_y"`
	EstaViva bool   `json:"esta_viva"`
}

type GameResponse struct {
	ID          int32           `json:"id"`
	TurnoActual string          `json:"turno_actual"`
	Estado      string          `json:"estado"`
	Piezas      []PieceResponse `json:"piezas"`
	InstanceID  string          `json:"instance_id"`
}

type MoveRequest struct {
	GameID   int32  `json:"game_id"`
	IDPieza  int32  `json:"id_pieza"`
	DestinoX string `json:"destino_x"`
	DestinoY int32  `json:"destino_y"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type BackendStatus struct {
	Address    string `json:"address"`
	InstanceID string `json:"instance_id"`
	Healthy    bool   `json:"healthy"`
}

type GatewayStatus struct {
	Status   string          `json:"status"`
	Backends []BackendStatus `json:"backends"`
}

func main() {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = defaultPort
	}

	if _, err := strconv.Atoi(port); err != nil {
		log.Fatalf("PORT inválido: %s", port)
	}

	gameID := int32(defaultGameID)

	if configuredID := strings.TrimSpace(os.Getenv("GAME_ID")); configuredID != "" {
		parsedID, err := strconv.ParseInt(configuredID, 10, 32)
		if err != nil || parsedID <= 0 {
			log.Fatalf("GAME_ID inválido: %s", configuredID)
		}
		gameID = int32(parsedID)
	}

	addresses := loadBackendAddresses()
	if len(addresses) == 0 {
		log.Fatal("No hay backends gRPC configurados")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if len(jwtSecret) < 32 {
		log.Fatal("JWT_SECRET debe tener al menos 32 caracteres")
	}

	usersAddress := strings.TrimSpace(os.Getenv("USERS_SERVICE_ADDRESS"))
	if usersAddress == "" {
		usersAddress = "users-service:50054"
	}

	usersConnection, err := grpc.NewClient(
		usersAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("No se pudo crear la conexión con Users Service: %v", err)
	}

	gateway := &Gateway{
		gameID:          gameID,
		usersConnection: usersConnection,
		usersClient:     pb.NewUserServiceClient(usersConnection),
		jwtSecret:       []byte(jwtSecret),
	}

	defer func() {
		if err := usersConnection.Close(); err != nil {
			log.Printf("Error cerrando conexión con Users Service: %v", err)
		}
	}()

	log.Printf("Users Service configurado: %s", usersAddress)

	for index, address := range addresses {
		connection, err := grpc.NewClient(
			address,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			for _, backend := range gateway.backends {
				_ = backend.Connection.Close()
			}
			log.Fatalf("No se pudo crear la conexión con %s: %v", address, err)
		}

		backend := &Backend{
			Address:    address,
			InstanceID: fmt.Sprintf("game-service-node-%02d", index+1),
			Connection: connection,
			Client:     pb.NewGameServiceClient(connection),
		}

		gateway.backends = append(gateway.backends, backend)
		log.Printf("Backend configurado: %s", address)
	}

	defer func() {
		for _, backend := range gateway.backends {
			if err := backend.Connection.Close(); err != nil {
				log.Printf("Error cerrando conexión con %s: %v", backend.Address, err)
			}
		}
	}()

	gateway.checkAllBackends()
	go gateway.startHealthChecks()

	mux := http.NewServeMux()

	mux.HandleFunc("GET /heartbeat", gateway.handleHeartbeat)
	mux.HandleFunc("GET /api/juego", gateway.handleGetGame)
	mux.Handle("POST /api/mover", gateway.requireAuth(http.HandlerFunc(gateway.handleMakeMove)))
	mux.Handle("POST /api/reiniciar", gateway.requireAuth(http.HandlerFunc(gateway.handleResetGame)))
	mux.HandleFunc("GET /api/status", gateway.handleStatus)
	mux.HandleFunc("POST /api/usuarios/registro", gateway.handleRegisterUser)
	mux.HandleFunc("POST /api/usuarios/login", gateway.handleLoginUser)
	mux.HandleFunc("GET /api/usuarios/{id}", gateway.handleGetUser)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		log.Fatalf("Error abriendo el puerto %s: %v", server.Addr, err)
	}

	log.Printf("API Gateway escuchando en el puerto %s", port)

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.Serve(listener)
	}()

	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalChannel)

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Error ejecutando API Gateway: %v", err)
		}

	case receivedSignal := <-signalChannel:
		log.Printf("Señal %s recibida. Cerrando API Gateway...", receivedSignal)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Cierre ordenado fallido: %v", err)
			if closeErr := server.Close(); closeErr != nil {
				log.Printf("Error cerrando servidor HTTP: %v", closeErr)
			}
		}
	}
}

func loadBackendAddresses() []string {
	configured := strings.TrimSpace(os.Getenv("GAME_BACKENDS"))

	if configured == "" {
		configured = "game-service-1:50053,game-service-2:50053"
	}

	parts := strings.Split(configured, ",")
	addresses := make([]string, 0, len(parts))
	seen := make(map[string]bool)

	for _, part := range parts {
		address := strings.TrimSpace(part)
		if address == "" {
			continue
		}
		if seen[address] {
			log.Printf("Backend duplicado ignorado: %s", address)
			continue
		}

		seen[address] = true
		addresses = append(addresses, address)
	}

	return addresses
}

func (g *Gateway) checkAllBackends() {
	for _, backend := range g.backends {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			healthCheckTimeout,
		)

		_, err := backend.Client.GetGameState(ctx, &pb.GetGameRequest{
			GameId: g.gameID,
		})

		cancel()

		backend.SetHealthy(err == nil)

		if err != nil {
			log.Printf("Backend no disponible: %s: %v", backend.Address, err)
		} else {
			log.Printf("Backend disponible: %s", backend.Address)
		}
	}
}

func (g *Gateway) startHealthChecks() {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for range ticker.C {
		g.checkAllBackends()
	}
}

// healthyBackends devuelve las instancias saludables en orden circular.
// El contador distribuye las solicitudes entre los backends disponibles.
func (g *Gateway) healthyBackends() []*Backend {
	total := len(g.backends)
	if total == 0 {
		return nil
	}

	start := int((g.counter.Add(1) - 1) % uint64(total))
	available := make([]*Backend, 0, total)

	for offset := 0; offset < total; offset++ {
		index := (start + offset) % total
		backend := g.backends[index]

		if backend.IsHealthy() {
			available = append(available, backend)
		}
	}

	return available
}

func (g *Gateway) execute(
	ctx context.Context,
	operation func(context.Context, *Backend) (*pb.GameState, error),
	retryOnTransportFailure bool,
) (*pb.GameState, *Backend, error) {
	candidates := g.healthyBackends()

	if len(candidates) == 0 {
		return nil, nil, errors.New("no hay backends saludables disponibles")
	}

	attempts := len(candidates)
	if !retryOnTransportFailure {
		attempts = 1
	}

	var lastError error

	for index := 0; index < attempts; index++ {
		backend := candidates[index]

		requestContext, cancel := context.WithTimeout(
			ctx,
			defaultRequestTimeout,
		)

		result, err := operation(requestContext, backend)
		cancel()

		if err == nil {
			backend.SetHealthy(true)
			return result, backend, nil
		}

		lastError = err
		code := status.Code(err)

		if code == codes.Unavailable || code == codes.DeadlineExceeded {
			backend.SetHealthy(false)

			log.Printf(
				"Fallo de comunicación con %s: %v",
				backend.Address,
				err,
			)

			// Las escrituras nunca se repiten automáticamente.
			// Un timeout no permite saber si PostgreSQL confirmó la operación.
			if !retryOnTransportFailure {
				return nil, backend, err
			}

			continue
		}

		// Los errores funcionales, como un movimiento inválido,
		// no justifican probar otra instancia.
		return nil, backend, err
	}

	if lastError != nil {
		return nil, nil, lastError
	}

	return nil, nil, errors.New("no fue posible ejecutar la operación")
}

func (g *Gateway) handleGetGame(w http.ResponseWriter, r *http.Request) {
	result, backend, err := g.execute(
		r.Context(),
		func(ctx context.Context, backend *Backend) (*pb.GameState, error) {
			return backend.Client.GetGameState(ctx, &pb.GetGameRequest{
				GameId: g.gameID,
			})
		},
		true,
	)

	if err != nil {
		writeGRPCError(w, err)
		return
	}

	writeGameResponse(w, result, backend)
}

func (g *Gateway) handleMakeMove(w http.ResponseWriter, r *http.Request) {
	var request MoveRequest

	if err := decodeJSONBody(w, r, &request); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	if request.IDPieza <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Identificador de pieza inválido")
		return
	}

	requestGameID := request.GameID
	if requestGameID == 0 {
		requestGameID = g.gameID
	}

	if requestGameID <= 0 || requestGameID != g.gameID {
		writeJSONError(w, http.StatusBadRequest, "Identificador de partida no permitido")
		return
	}

	request.DestinoX = strings.ToUpper(strings.TrimSpace(request.DestinoX))

	if len(request.DestinoX) != 1 ||
		request.DestinoX[0] < 'A' ||
		request.DestinoX[0] > 'E' ||
		request.DestinoY < 1 ||
		request.DestinoY > 5 {
		writeJSONError(w, http.StatusBadRequest, "Destino fuera del tablero")
		return
	}

	result, backend, err := g.execute(
		r.Context(),
		func(ctx context.Context, backend *Backend) (*pb.GameState, error) {
			return backend.Client.MakeMove(ctx, &pb.MoveRequest{
				GameId:   requestGameID,
				IdPieza:  request.IDPieza,
				DestinoX: request.DestinoX,
				DestinoY: request.DestinoY,
			})
		},
		false,
	)

	if err != nil {
		writeGRPCError(w, err)
		return
	}

	writeGameResponse(w, result, backend)
}

func (g *Gateway) handleResetGame(w http.ResponseWriter, r *http.Request) {
	result, backend, err := g.execute(
		r.Context(),
		func(ctx context.Context, backend *Backend) (*pb.GameState, error) {
			return backend.Client.ResetGame(ctx, &pb.GetGameRequest{
				GameId: g.gameID,
			})
		},
		false,
	)

	if err != nil {
		writeGRPCError(w, err)
		return
	}

	writeGameResponse(w, result, backend)
}

func (g *Gateway) handleStatus(w http.ResponseWriter, r *http.Request) {
	backends := make([]BackendStatus, 0, len(g.backends))
	healthyCount := 0

	for _, backend := range g.backends {
		healthy := backend.IsHealthy()
		if healthy {
			healthyCount++
		}

		backends = append(backends, BackendStatus{
			Address:    backend.Address,
			InstanceID: backend.InstanceID,
			Healthy:    healthy,
		})
	}

	result := GatewayStatus{
		Status:   "healthy",
		Backends: backends,
	}

	if healthyCount == 0 {
		result.Status = "unavailable"
	} else if healthyCount < len(g.backends) {
		result.Status = "degraded"
	}

	writeJSON(w, http.StatusOK, result)
}

func (g *Gateway) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// decodeJSONBody limita el tamaño de la solicitud y exige un único objeto JSON.
// También rechaza campos desconocidos para detectar errores de integración.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return errors.New("el cuerpo de la solicitud supera el tamaño permitido")
		}
		return errors.New("solicitud JSON inválida")
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("la solicitud debe contener un único objeto JSON")
	}

	return nil
}

func writeGameResponse(
	w http.ResponseWriter,
	game *pb.GameState,
	backend *Backend,
) {
	pieces := make([]PieceResponse, 0, len(game.Piezas))

	for _, piece := range game.Piezas {
		if piece == nil {
			continue
		}

		pieces = append(pieces, PieceResponse{
			ID:       piece.Id,
			Jugador:  piece.Jugador,
			Tipo:     piece.Tipo,
			PosX:     piece.PosX,
			PosY:     piece.PosY,
			EstaViva: piece.EstaViva,
		})
	}

	instanceID := game.InstanceId
	if instanceID == "" {
		instanceID = backend.InstanceID
	}

	response := GameResponse{
		ID:          game.Id,
		TurnoActual: game.TurnoActual,
		Estado:      game.Estado,
		Piezas:      pieces,
		InstanceID:  instanceID,
	}

	// Serializar antes de enviar cabeceras evita respuestas parciales.
	body, err := json.Marshal(response)
	if err != nil {
		log.Printf("Error serializando respuesta: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "Error al generar la respuesta")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Instance-Id", instanceID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bytes.TrimSpace(body))
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		log.Printf("Error serializando respuesta JSON: %v", err)
		http.Error(w, "Error interno del servidor", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

func writeJSONError(w http.ResponseWriter, statusCode int, message string) {
	writeJSON(w, statusCode, ErrorResponse{
		Error: message,
	})
}

func writeGRPCError(w http.ResponseWriter, err error) {
	code := status.Code(err)
	message := status.Convert(err).Message()

	switch code {
	case codes.InvalidArgument:
		writeJSONError(w, http.StatusBadRequest, message)

	case codes.FailedPrecondition:
		writeJSONError(w, http.StatusConflict, message)

	case codes.PermissionDenied:
		writeJSONError(w, http.StatusForbidden, message)

	case codes.Unauthenticated:
		writeJSONError(w, http.StatusUnauthorized, message)

	case codes.NotFound:
		writeJSONError(w, http.StatusNotFound, message)

	case codes.AlreadyExists, codes.Aborted:
		writeJSONError(w, http.StatusConflict, message)

	case codes.ResourceExhausted:
		writeJSONError(w, http.StatusTooManyRequests, message)

	case codes.Unavailable, codes.DeadlineExceeded:
		writeJSONError(
			w,
			http.StatusServiceUnavailable,
			"Servicio de juego no disponible",
		)

	default:
		log.Printf("Error gRPC: %v", err)
		writeJSONError(
			w,
			http.StatusInternalServerError,
			"Error interno del servidor",
		)
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
