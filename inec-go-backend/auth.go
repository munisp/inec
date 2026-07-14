package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"github.com/rs/zerolog/log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/pbkdf2"
)

var jwtSecret []byte

func init() {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		if os.Getenv("INEC_ENV") == "development" || os.Getenv("INEC_ENV") == "" {
			log.Warn().Msg("JWT_SECRET not set — generating ephemeral key (dev mode only)")
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				log.Error().Err(err).Msg("failed to generate random JWT secret")
			}
			s = base64.RawURLEncoding.EncodeToString(b)
		} else {
			log.Error().Msg("JWT_SECRET environment variable is required in production (set INEC_ENV=development to allow ephemeral keys)")
		}
	}
	if len(s) < 32 {
		log.Error().Msg("JWT_SECRET must be at least 32 characters")
	}
	jwtSecret = []byte(s)
}

func hashPassword(password string) string {
	h, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h)
}

func verifyPassword(password, hashed string) bool {
	if strings.HasPrefix(hashed, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(password)) == nil
	}
	if strings.HasPrefix(hashed, "$pbkdf2-sha256$") {
		return verifyPasslib(password, hashed)
	}
	return false
}

func verifyPasslib(password, hashed string) bool {
	parts := strings.Split(hashed, "$")
	if len(parts) != 5 {
		return false
	}
	rounds, err := strconv.Atoi(parts[2])
	if err != nil {
		return false
	}
	saltB64 := parts[3]
	hashB64 := parts[4]
	salt, err := ab64Decode(saltB64)
	if err != nil {
		return false
	}
	expected, err := ab64Decode(hashB64)
	if err != nil {
		return false
	}
	derived := pbkdf2.Key([]byte(password), salt, rounds, len(expected), sha256.New)
	return hmac.Equal(derived, expected)
}

func ab64Decode(s string) ([]byte, error) {
	s = strings.ReplaceAll(s, ".", "+")
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.StdEncoding.DecodeString(s)
}

func createAccessToken(claims map[string]interface{}) (string, error) {
	mc := jwt.MapClaims{}
	for k, v := range claims {
		mc[k] = v
	}
	mc["exp"] = time.Now().Add(1 * time.Hour).Unix()
	mc["type"] = "access"
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, mc)
	return token.SignedString(jwtSecret)
}

func createRefreshToken(claims map[string]interface{}) (string, error) {
	mc := jwt.MapClaims{}
	for k, v := range claims {
		mc[k] = v
	}
	mc["exp"] = time.Now().Add(7 * 24 * time.Hour).Unix()
	mc["type"] = "refresh"
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, mc)
	return token.SignedString(jwtSecret)
}

func decodeToken(tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		return claims, nil
	}
	return nil, fmt.Errorf("invalid token")
}

func getCurrentUser(r *http.Request) (jwt.MapClaims, error) {
	auth := r.Header.Get("Authorization")
	if auth == "" || !strings.HasPrefix(auth, "Bearer ") {
		return nil, fmt.Errorf("not authenticated")
	}
	return decodeToken(strings.TrimPrefix(auth, "Bearer "))
}

func requireRole(r *http.Request, roles ...string) (jwt.MapClaims, error) {
	user, err := getCurrentUser(r)
	if err != nil {
		return nil, err
	}
	role, _ := user["role"].(string)
	for _, allowed := range roles {
		if role == allowed {
			return user, nil
		}
	}
	return nil, fmt.Errorf("insufficient permissions")
}
