package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	pb "mini-chess/proto/pb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func main() {
	// Conectamos con Users Service a través del puerto local.
	conn, err := grpc.NewClient(
		"localhost:50054",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("No se pudo crear el cliente gRPC: %v", err)
	}
	defer conn.Close()

	client := pb.NewUserServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Generamos datos únicos para no colisionar con otras pruebas.
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	username := "smoketest_" + suffix
	email := "smoketest_" + suffix + "@example.test"
	password := "MiniChess-Test-2026!"

	// 1. Registro de usuario.
	register, err := client.RegisterUser(ctx, &pb.RegisterUserRequest{
		Username: username,
		Email:    email,
		Password: password,
	})
	if err != nil {
		log.Fatalf("FALLO en RegisterUser: %v", err)
	}
	if register.User == nil || register.User.Id == 0 {
		log.Fatal("FALLO: el registro no devolvió un usuario válido")
	}

	fmt.Println("OK: RegisterUser")
	fmt.Printf("    ID: %d\n", register.User.Id)
	fmt.Printf("    Username: %s\n", register.User.Username)

	// 2. Inicio de sesión usando el correo.
	login, err := client.LoginUser(ctx, &pb.LoginUserRequest{
		Identifier: email,
		Password:   password,
	})
	if err != nil {
		log.Fatalf("FALLO en LoginUser: %v", err)
	}
	if login.User == nil || login.User.Id != register.User.Id {
		log.Fatal("FALLO: el usuario del login no coincide con el registrado")
	}
	if login.AccessToken == "" || !strings.EqualFold(login.TokenType, "Bearer") {
		log.Fatal("FALLO: el login no devolvió un token Bearer válido")
	}

	fmt.Println("OK: LoginUser")
	fmt.Printf("    Token recibido: sí (%d caracteres; no se muestra)\n", len(login.AccessToken))
	fmt.Printf("    Tipo: %s\n", login.TokenType)
	fmt.Printf("    Expiración: %d segundos\n", login.ExpiresInSeconds)

	// 3. Consulta del usuario por su ID.
	user, err := client.GetUser(ctx, &pb.GetUserRequest{
		UserId: register.User.Id,
	})
	if err != nil {
		log.Fatalf("FALLO en GetUser: %v", err)
	}
	if user.Id != register.User.Id || user.Username != username {
		log.Fatal("FALLO: los datos consultados no coinciden")
	}

	fmt.Println("OK: GetUser")

	// 4. Comprobamos que una contraseña incorrecta sea rechazada.
	_, err = client.LoginUser(ctx, &pb.LoginUserRequest{
		Identifier: email,
		Password:   "Wrong-Password-123!",
	})
	if status.Code(err) != codes.Unauthenticated {
		if err == nil {
			log.Fatal("FALLO: se aceptó una contraseña incorrecta")
		}
		log.Fatalf("FALLO: código inesperado para contraseña incorrecta: %v", err)
	}

	fmt.Println("OK: contraseña incorrecta rechazada")

	fmt.Println()
	fmt.Println("RESULTADO: todas las pruebas finalizaron correctamente.")
}
