package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type KeycloakUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
	Realm    string `json:"realm"`
}

type KeycloakClient interface {
	ValidateToken(ctx context.Context, token string) (*KeycloakUser, error)
	GetUserInfo(ctx context.Context, token string) (*KeycloakUser, error)
	IntrospectToken(ctx context.Context, token string) (map[string]interface{}, error)
	Status() MWStatus
	Close() error
}

type keycloakHTTPClient struct {
	baseURL      string
	realm        string
	clientID     string
	clientSecret string
	client       *http.Client
}

func (k *keycloakHTTPClient) ValidateToken(ctx context.Context, token string) (*KeycloakUser, error) {
	url := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/userinfo", k.baseURL, k.realm)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("token validation failed: %d", resp.StatusCode)
	}
	var info map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&info)
	user := &KeycloakUser{
		ID:       fmt.Sprintf("%v", info["sub"]),
		Username: fmt.Sprintf("%v", info["preferred_username"]),
		Email:    fmt.Sprintf("%v", info["email"]),
		FullName: fmt.Sprintf("%v", info["name"]),
		Realm:    k.realm,
	}
	if roles, ok := info["realm_access"].(map[string]interface{}); ok {
		if roleList, ok := roles["roles"].([]interface{}); ok {
			for _, r := range roleList {
				rs := fmt.Sprintf("%v", r)
				if rs == "admin" || rs == "presiding_officer" || rs == "collation_officer" || rs == "observer" {
					user.Role = rs
					break
				}
			}
		}
	}
	if user.Role == "" {
		user.Role = "public"
	}
	return user, nil
}

func (k *keycloakHTTPClient) GetUserInfo(ctx context.Context, token string) (*KeycloakUser, error) {
	return k.ValidateToken(ctx, token)
}

func (k *keycloakHTTPClient) IntrospectToken(ctx context.Context, token string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/token/introspect", k.baseURL, k.realm)
	body := fmt.Sprintf("token=%s&client_id=%s&client_secret=%s", token, k.clientID, k.clientSecret)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

func (k *keycloakHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s/realms/%s/.well-known/openid-configuration", k.baseURL, k.realm)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	lat, err := measureLatency(func() error {
		resp, e := k.client.Do(req)
		if e != nil {
			return e
		}
		resp.Body.Close()
		return nil
	})
	if err != nil {
		return MWStatus{Name: "Keycloak", Connected: false, Mode: "external (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "Keycloak", Connected: true, Mode: "external", Latency: fmtLatency(lat), Details: "realm: " + k.realm}
}

func (k *keycloakHTTPClient) Close() error { return nil }

type embeddedKeycloak struct{}

func (k *embeddedKeycloak) ValidateToken(_ context.Context, token string) (*KeycloakUser, error) {
	claims, err := decodeToken(token)
	if err != nil {
		return nil, err
	}
	return &KeycloakUser{
		ID:       fmt.Sprintf("%v", claims["sub"]),
		Username: fmt.Sprintf("%v", claims["username"]),
		FullName: fmt.Sprintf("%v", claims["full_name"]),
		Role:     fmt.Sprintf("%v", claims["role"]),
		Realm:    "inec-local",
	}, nil
}

func (k *embeddedKeycloak) GetUserInfo(ctx context.Context, token string) (*KeycloakUser, error) {
	return k.ValidateToken(ctx, token)
}

func (k *embeddedKeycloak) IntrospectToken(_ context.Context, token string) (map[string]interface{}, error) {
	claims, err := decodeToken(token)
	if err != nil {
		return nil, err
	}
	result := map[string]interface{}{
		"active":   true,
		"sub":      claims["sub"],
		"username": claims["username"],
		"role":     claims["role"],
	}
	return result, nil
}

func (k *embeddedKeycloak) Status() MWStatus {
	return MWStatus{
		Name: "Keycloak", Connected: true, Mode: "embedded",
		Latency: "0.0ms",
		Details: "local JWT validation (HMAC-SHA256), realm: inec-local",
	}
}

func (k *embeddedKeycloak) Close() error { return nil }

func initKeycloakClient() KeycloakClient {
	kcURL := envOrDefault("KEYCLOAK_URL", "")
	if kcURL != "" {
		realm := envOrDefault("KEYCLOAK_REALM", "inec")
		clientID := envOrDefault("KEYCLOAK_CLIENT_ID", "inec-backend")
		clientSecret := envOrDefault("KEYCLOAK_CLIENT_SECRET", "")
		client := &keycloakHTTPClient{
			baseURL:      kcURL,
			realm:        realm,
			clientID:     clientID,
			clientSecret: clientSecret,
			client:       &http.Client{Timeout: 5 * time.Second},
		}
		s := client.Status()
		if s.Connected {
			log.Println("[Keycloak] Connected to external Keycloak at", kcURL, "realm:", realm)
			return client
		}
		log.Println("[Keycloak] External Keycloak unreachable, falling back to local JWT")
	}
	log.Println("[Keycloak] Using embedded local JWT validation")
	return &embeddedKeycloak{}
}
