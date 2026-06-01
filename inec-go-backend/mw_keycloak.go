package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Nerzal/gocloak/v13"
	"github.com/rs/zerolog/log"
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
	client       *ResilientHTTPClient
}

func (k *keycloakHTTPClient) ValidateToken(ctx context.Context, token string) (*KeycloakUser, error) {
	url := fmt.Sprintf("%s/realms/%s/protocol/openid-connect/userinfo", k.baseURL, k.realm)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
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
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
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
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := k.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("introspect token: %w", err)
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result, nil
}

func (k *keycloakHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s/realms/%s/.well-known/openid-configuration", k.baseURL, k.realm)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return MWStatus{Name: "Keycloak", Connected: false, Mode: "external (error)", Details: err.Error()}
	}
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

// --- Real Keycloak client using gocloak ---

type goClockKeycloak struct {
	client       *gocloak.GoCloak
	realm        string
	clientID     string
	clientSecret string
}

func newGoClockKeycloak(baseURL, realm, clientID, clientSecret string) *goClockKeycloak {
	client := gocloak.NewClient(baseURL)
	return &goClockKeycloak{
		client:       client,
		realm:        realm,
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

func (g *goClockKeycloak) ValidateToken(ctx context.Context, token string) (*KeycloakUser, error) {
	rptResult, err := g.client.RetrospectToken(ctx, token, g.clientID, g.clientSecret, g.realm)
	if err != nil {
		return nil, fmt.Errorf("token introspection failed: %w", err)
	}
	if rptResult.Active == nil || !*rptResult.Active {
		return nil, fmt.Errorf("token is not active")
	}
	info, err := g.client.GetUserInfo(ctx, token, g.realm)
	if err != nil {
		return nil, fmt.Errorf("get user info failed: %w", err)
	}
	user := &KeycloakUser{
		Realm: g.realm,
		Role:  "public",
	}
	if info.Sub != nil {
		user.ID = *info.Sub
	}
	if info.PreferredUsername != nil {
		user.Username = *info.PreferredUsername
	}
	if info.Email != nil {
		user.Email = *info.Email
	}
	if info.Name != nil {
		user.FullName = *info.Name
	}
	return user, nil
}

func (g *goClockKeycloak) GetUserInfo(ctx context.Context, token string) (*KeycloakUser, error) {
	return g.ValidateToken(ctx, token)
}

func (g *goClockKeycloak) IntrospectToken(ctx context.Context, token string) (map[string]interface{}, error) {
	rptResult, err := g.client.RetrospectToken(ctx, token, g.clientID, g.clientSecret, g.realm)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"active": rptResult.Active != nil && *rptResult.Active,
	}, nil
}

func (g *goClockKeycloak) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	lat, err := measureLatency(func() error {
		_, e := g.client.GetServerInfo(ctx, func() string {
			token, _ := g.client.LoginClient(ctx, g.clientID, g.clientSecret, g.realm)
			if token != nil {
				return token.AccessToken
			}
			return ""
		}())
		return e
	})
	if err != nil {
		return MWStatus{Name: "Keycloak", Connected: false, Mode: "gocloak (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "Keycloak", Connected: true, Mode: "native gocloak", Latency: fmtLatency(lat), Details: "realm: " + g.realm + ", OIDC, token introspection"}
}

func (g *goClockKeycloak) Close() error { return nil }

// --- Init ---

func initKeycloakClient() KeycloakClient {
	kcURL := envOrDefault("KEYCLOAK_URL", "")
	if kcURL != "" {
		realm := envOrDefault("KEYCLOAK_REALM", "inec")
		clientID := envOrDefault("KEYCLOAK_CLIENT_ID", "inec-backend")
		clientSecret := envOrDefault("KEYCLOAK_CLIENT_SECRET", "")

		// Try gocloak first
		if clientSecret != "" {
			gc := newGoClockKeycloak(kcURL, realm, clientID, clientSecret)
			s := gc.Status()
			if s.Connected {
				log.Info().Str("url", kcURL).Str("realm", realm).Msg("Keycloak connected via gocloak")
				return gc
			}
		}

		// Fall back to HTTP client
		client := &keycloakHTTPClient{
			baseURL:      kcURL,
			realm:        realm,
			clientID:     clientID,
			clientSecret: clientSecret,
			client:       NewResilientHTTPClient("keycloak"),
		}
		s := client.Status()
		if s.Connected {
			log.Info().Str("url", kcURL).Str("realm", realm).Msg("Keycloak connected via HTTP")
			return client
		}
		log.Warn().Str("url", kcURL).Msg("Keycloak unreachable, falling back to local JWT")
	}
	log.Info().Msg("Keycloak using embedded local JWT validation")
	return &embeddedKeycloak{}
}
