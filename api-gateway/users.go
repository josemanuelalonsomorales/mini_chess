package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "mini-chess/proto/pb"
)

const jwtIssuer = "mini-chess-users-service"

type userClaims struct {
	UserID   int32  `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

type userResponse struct {
	ID       int32  `json:"id"`
	Username string `json:"username"`
	Rating   int32  `json:"rating"`
}

type registerRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

type loginResponse struct {
	User             userResponse `json:"user"`
	AccessToken      string       `json:"access_token"`
	TokenType        string       `json:"token_type"`
	ExpiresInSeconds int64        `json:"expires_in_seconds"`
}

func mapUser(user *pb.User) userResponse {
	return userResponse{
		ID:       user.GetId(),
		Username: user.GetUsername(),
		Rating:   user.GetRating(),
	}
}

func (g *Gateway) handleRegisterUser(w http.ResponseWriter, r *http.Request) {
	var request registerRequest

	if err := decodeJSONBody(w, r, &request); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), defaultRequestTimeout)
	defer cancel()

	result, err := g.usersClient.RegisterUser(ctx, &pb.RegisterUserRequest{
		Username: request.Username,
		Email:    request.Email,
		Password: request.Password,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, mapUser(result.GetUser()))
}

func (g *Gateway) handleLoginUser(w http.ResponseWriter, r *http.Request) {
	var request loginRequest

	if err := decodeJSONBody(w, r, &request); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), defaultRequestTimeout)
	defer cancel()

	result, err := g.usersClient.LoginUser(ctx, &pb.LoginUserRequest{
		Identifier: request.Identifier,
		Password:   request.Password,
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	if result.GetUser() == nil || result.GetAccessToken() == "" {
		writeJSONError(w, http.StatusInternalServerError, "Respuesta de autenticación inválida")
		return
	}

	writeJSON(w, http.StatusOK, loginResponse{
		User:             mapUser(result.GetUser()),
		AccessToken:      result.GetAccessToken(),
		TokenType:        result.GetTokenType(),
		ExpiresInSeconds: result.GetExpiresInSeconds(),
	})
}

func (g *Gateway) handleGetUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 32)
	if err != nil || id <= 0 {
		writeJSONError(w, http.StatusBadRequest, "Identificador de usuario inválido")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), defaultRequestTimeout)
	defer cancel()

	user, err := g.usersClient.GetUser(ctx, &pb.GetUserRequest{
		UserId: int32(id),
	})
	if err != nil {
		writeGRPCError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, mapUser(user))
}

func (g *Gateway) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		parts := strings.Fields(header)

		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeJSONError(w, http.StatusUnauthorized, "Se requiere autenticación")
			return
		}

		claims := &userClaims{}

		token, err := jwt.ParseWithClaims(
			parts[1],
			claims,
			func(token *jwt.Token) (any, error) {
				if token.Method != jwt.SigningMethodHS256 {
					return nil, errors.New("algoritmo JWT no permitido")
				}
				return g.jwtSecret, nil
			},
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
			jwt.WithIssuer(jwtIssuer),
			jwt.WithExpirationRequired(),
			jwt.WithIssuedAt(),
		)

		if err != nil || token == nil || !token.Valid {
			writeJSONError(w, http.StatusUnauthorized, "Token inválido o expirado")
			return
		}

		if claims.UserID <= 0 ||
			strings.TrimSpace(claims.Username) == "" ||
			claims.IssuedAt == nil ||
			claims.ExpiresAt == nil ||
			claims.Subject != fmt.Sprintf("%d", claims.UserID) {
			writeJSONError(w, http.StatusUnauthorized, "Token inválido")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Conserva una referencia explícita al tipo de error gRPC para los handlers.
var _ = codes.OK
var _ = status.Code
