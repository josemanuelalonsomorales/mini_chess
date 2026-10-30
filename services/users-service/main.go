
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "mini-chess/proto/pb"
)

const (
	defaultPort       = "50054"
	tokenExpiration   = 24 * time.Hour
	bcryptCost        = 12
	maxPasswordLength = 72
)

var (
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,50}$`)
	emailRegex    = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

type usersServer struct {
	pb.UnimplementedUserServiceServer
	db        *sql.DB
	jwtSecret []byte
	publisher *kafkaPublisher
}

// claims define la información firmada dentro del JWT.
type claims struct {
	UserID   int32  `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("Users Service terminó con error: %v", err)
	}
}

func run() error {
	port := envOrDefault("PORT", defaultPort)
	db, err := connectDatabase()
	if err != nil {
		return fmt.Errorf("conexión a PostgreSQL: %w", err)
	}
	defer db.Close()

	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return errors.New("JWT_SECRET debe tener al menos 32 caracteres")
	}

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("no se pudo abrir el puerto %s: %w", port, err)
	}

	publisher := newKafkaPublisher()
	defer publisher.close()

	grpcServer := grpc.NewServer()
	pb.RegisterUserServiceServer(grpcServer, &usersServer{
		db:        db,
		jwtSecret: []byte(secret),
		publisher: publisher,
	})

	serveErr := make(chan error, 1)
	go func() {
		log.Printf("Users Service escuchando en :%s", port)
		serveErr <- grpcServer.Serve(listener)
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("servidor gRPC: %w", err)
		}
		return nil
	case <-stop:
		log.Println("Cerrando Users Service...")
	}

	grpcServer.GracefulStop()
	return nil
}

func connectDatabase() (*sql.DB, error) {
	host := envOrDefault("DB_HOST", "localhost")
	port := envOrDefault("DB_PORT", "5432")
	user := envOrDefault("DB_USER", "postgres")
	password := os.Getenv("DB_PASSWORD")
	name := envOrDefault("DB_NAME", "minichess")

	if password == "" {
		return nil, errors.New("DB_PASSWORD no está configurada")
	}

	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, name,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	for attempt := 1; attempt <= 30; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = db.PingContext(ctx)
		cancel()

		if err == nil {
			log.Println("Conexión a PostgreSQL establecida")
			return db, nil
		}

		log.Printf("Esperando PostgreSQL (%d/30): %v", attempt, err)
		time.Sleep(2 * time.Second)
	}

	db.Close()
	return nil, fmt.Errorf("PostgreSQL no respondió: %w", err)
}

func (s *usersServer) RegisterUser(
	ctx context.Context,
	req *pb.RegisterUserRequest,
) (*pb.RegisterUserResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "solicitud vacía")
	}

	username := strings.TrimSpace(req.GetUsername())
	email := strings.ToLower(strings.TrimSpace(req.GetEmail()))
	password := req.GetPassword()

	if !usernameRegex.MatchString(username) {
		return nil, status.Error(
			codes.InvalidArgument,
			"el nombre debe tener entre 3 y 50 caracteres y solo admite letras, números y guion bajo",
		)
	}

	if len(email) > 100 || !emailRegex.MatchString(email) {
		return nil, status.Error(codes.InvalidArgument, "correo electrónico no válido")
	}

	if len(password) < 8 || len(password) > maxPasswordLength {
		return nil, status.Error(
			codes.InvalidArgument,
			"la contraseña debe tener entre 8 y 72 bytes",
		)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		log.Printf("Error generando hash: %v", err)
		return nil, status.Error(codes.Internal, "no se pudo procesar la contraseña")
	}

	var user pb.User
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO users (username, email, password_hash)
		VALUES ($1, $2, $3)
		RETURNING id, username, rating
	`, username, email, string(hash)).Scan(
		&user.Id,
		&user.Username,
		&user.Rating,
	)

	if err != nil {
		var pgErr *pq.Error
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, status.Error(
				codes.AlreadyExists,
				"el nombre de usuario o correo ya está registrado",
			)
		}

		log.Printf("Error registrando usuario: %v", err)
		return nil, status.Error(codes.Internal, "no se pudo registrar el usuario")
	}

	s.publisher.publishUserRegistered(user.Id, user.Username)
	
	return &pb.RegisterUserResponse{User: &user}, nil
}

func (s *usersServer) LoginUser(
	ctx context.Context,
	req *pb.LoginUserRequest,
) (*pb.LoginUserResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "solicitud vacía")
	}

	identifier := strings.TrimSpace(req.GetIdentifier())
	password := req.GetPassword()

	if identifier == "" || password == "" {
		return nil, status.Error(codes.InvalidArgument, "credenciales incompletas")
	}

	var user pb.User
	var passwordHash string

	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, rating, password_hash
		FROM users
		WHERE LOWER(username) = LOWER($1)
		   OR LOWER(email) = LOWER($1)
	`, identifier).Scan(
		&user.Id,
		&user.Username,
		&user.Rating,
		&passwordHash,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.Unauthenticated, "credenciales incorrectas")
		}

		log.Printf("Error consultando usuario: %v", err)
		return nil, status.Error(codes.Internal, "no se pudo iniciar sesión")
	}

	if err := bcrypt.CompareHashAndPassword(
		[]byte(passwordHash),
		[]byte(password),
	); err != nil {
		return nil, status.Error(codes.Unauthenticated, "credenciales incorrectas")
	}

	now := time.Now()
	expiration := now.Add(tokenExpiration)

	tokenClaims := claims{
		UserID:   user.Id,
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiration),
			Subject:   fmt.Sprintf("%d", user.Id),
			Issuer:    "mini-chess-users-service",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims)
	accessToken, err := token.SignedString(s.jwtSecret)
	if err != nil {
		log.Printf("Error generando JWT: %v", err)
		return nil, status.Error(codes.Internal, "no se pudo generar el token")
	}

	return &pb.LoginUserResponse{
		User:              &user,
		AccessToken:       accessToken,
		TokenType:         "Bearer",
		ExpiresInSeconds:  int64(tokenExpiration.Seconds()),
	}, nil
}

func (s *usersServer) GetUser(
	ctx context.Context,
	req *pb.GetUserRequest,
) (*pb.User, error) {
	if req == nil || req.GetUserId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "identificador de usuario no válido")
	}

	var user pb.User
	err := s.db.QueryRowContext(ctx, `
		SELECT id, username, rating
		FROM users
		WHERE id = $1
	`, req.GetUserId()).Scan(
		&user.Id,
		&user.Username,
		&user.Rating,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "usuario no encontrado")
		}

		log.Printf("Error consultando perfil: %v", err)
		return nil, status.Error(codes.Internal, "no se pudo consultar el usuario")
	}

	return &user, nil
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}
