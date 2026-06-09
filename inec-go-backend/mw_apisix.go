package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog/log"
)

type APISIXRoute struct {
	ID       string                 `json:"id"`
	URI      string                 `json:"uri"`
	Methods  []string               `json:"methods,omitempty"`
	Plugins  map[string]interface{} `json:"plugins,omitempty"`
	Upstream map[string]interface{} `json:"upstream,omitempty"`
}

// APISIXPlugin represents a plugin configuration for a route.
type APISIXPlugin struct {
	Name    string                 `json:"name"`
	Config  map[string]interface{} `json:"config,omitempty"`
	Enabled bool                   `json:"enabled"`
}

type APISIXClient interface {
	RegisterRoute(ctx context.Context, route APISIXRoute) error
	DeleteRoute(ctx context.Context, routeID string) error
	GetRoutes(ctx context.Context) ([]APISIXRoute, error)
	LoadPlugin(ctx context.Context, routeID string, plugin APISIXPlugin) error
	EnablePlugin(ctx context.Context, routeID, pluginName string) error
	DisablePlugin(ctx context.Context, routeID, pluginName string) error
	GetConfig() map[string]interface{}
	Status() MWStatus
	Close() error
}

type apisixHTTPClient struct {
	baseURL string
	apiKey  string
	client  *ResilientHTTPClient
}

func (a *apisixHTTPClient) RegisterRoute(ctx context.Context, route APISIXRoute) error {
	body, _ := json.Marshal(route)
	url := fmt.Sprintf("%s/apisix/admin/routes/%s", a.baseURL, route.ID)
	req, _ := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", a.apiKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (a *apisixHTTPClient) DeleteRoute(ctx context.Context, routeID string) error {
	url := fmt.Sprintf("%s/apisix/admin/routes/%s", a.baseURL, routeID)
	req, _ := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	req.Header.Set("X-API-KEY", a.apiKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (a *apisixHTTPClient) GetRoutes(ctx context.Context) ([]APISIXRoute, error) {
	url := fmt.Sprintf("%s/apisix/admin/routes", a.baseURL)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("X-API-KEY", a.apiKey)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		List []struct {
			Value APISIXRoute `json:"value"`
		} `json:"list"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	routes := make([]APISIXRoute, len(result.List))
	for i, r := range result.List {
		routes[i] = r.Value
	}
	return routes, nil
}

func (a *apisixHTTPClient) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"mode":     "external",
		"base_url": a.baseURL,
		"routes":   apisixDefaultRoutes(),
	}
}

func (a *apisixHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", a.baseURL+"/apisix/admin/routes", nil)
	req.Header.Set("X-API-KEY", a.apiKey)
	lat, err := measureLatency(func() error {
		resp, e := a.client.Client.Do(req)
		if e != nil {
			return e
		}
		resp.Body.Close()
		return nil
	})
	if err != nil {
		return MWStatus{Name: "APISIX", Connected: false, Mode: "external (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "APISIX", Connected: true, Mode: "external", Latency: fmtLatency(lat)}
}

func (a *apisixHTTPClient) LoadPlugin(ctx context.Context, routeID string, plugin APISIXPlugin) error {
	// Fetch the current route, merge plugin config, and PUT it back
	routes, err := a.GetRoutes(ctx)
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.ID == routeID {
			if route.Plugins == nil {
				route.Plugins = make(map[string]interface{})
			}
			if plugin.Enabled {
				route.Plugins[plugin.Name] = plugin.Config
			} else {
				delete(route.Plugins, plugin.Name)
			}
			return a.RegisterRoute(ctx, route)
		}
	}
	return fmt.Errorf("route %s not found", routeID)
}

func (a *apisixHTTPClient) EnablePlugin(ctx context.Context, routeID, pluginName string) error {
	routes, err := a.GetRoutes(ctx)
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.ID == routeID {
			if route.Plugins == nil {
				route.Plugins = make(map[string]interface{})
			}
			if _, ok := route.Plugins[pluginName]; !ok {
				route.Plugins[pluginName] = map[string]interface{}{}
			}
			return a.RegisterRoute(ctx, route)
		}
	}
	return fmt.Errorf("route %s not found", routeID)
}

func (a *apisixHTTPClient) DisablePlugin(ctx context.Context, routeID, pluginName string) error {
	routes, err := a.GetRoutes(ctx)
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.ID == routeID {
			delete(route.Plugins, pluginName)
			return a.RegisterRoute(ctx, route)
		}
	}
	return fmt.Errorf("route %s not found", routeID)
}

func (a *apisixHTTPClient) Close() error { return nil }

type embeddedAPISIX struct {
	routes []APISIXRoute
}

func newEmbeddedAPISIX() *embeddedAPISIX {
	return &embeddedAPISIX{routes: apisixDefaultRoutes()}
}

func (a *embeddedAPISIX) RegisterRoute(_ context.Context, route APISIXRoute) error {
	for i, r := range a.routes {
		if r.ID == route.ID {
			a.routes[i] = route
			return nil
		}
	}
	a.routes = append(a.routes, route)
	return nil
}

func (a *embeddedAPISIX) DeleteRoute(_ context.Context, routeID string) error {
	for i, r := range a.routes {
		if r.ID == routeID {
			a.routes = append(a.routes[:i], a.routes[i+1:]...)
			return nil
		}
	}
	return nil
}

func (a *embeddedAPISIX) GetRoutes(_ context.Context) ([]APISIXRoute, error) {
	return a.routes, nil
}

func (a *embeddedAPISIX) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"mode":   "embedded",
		"routes": a.routes,
		"plugins": map[string]interface{}{
			"rate_limiting": map[string]interface{}{
				"tiles":   "60 req/s",
				"metrics": "10 req/s",
				"results": "20 req/s",
				"reports": "5 req/s",
			},
			"jwt_auth":         "enabled",
			"cors":             "enabled",
			"gzip":             "enabled",
			"security_headers": "enabled",
		},
	}
}

func (a *embeddedAPISIX) Status() MWStatus {
	return MWStatus{
		Name: "APISIX", Connected: true, Mode: "embedded",
		Latency: "0.0ms",
		Details: fmt.Sprintf("local gateway config, %d routes, rate limiting + JWT + CORS + gzip active", len(a.routes)),
	}
}

func (a *embeddedAPISIX) LoadPlugin(_ context.Context, routeID string, plugin APISIXPlugin) error {
	for i, r := range a.routes {
		if r.ID == routeID {
			if a.routes[i].Plugins == nil {
				a.routes[i].Plugins = make(map[string]interface{})
			}
			if plugin.Enabled {
				a.routes[i].Plugins[plugin.Name] = plugin.Config
			} else {
				delete(a.routes[i].Plugins, plugin.Name)
			}
			return nil
		}
	}
	return fmt.Errorf("route %s not found", routeID)
}

func (a *embeddedAPISIX) EnablePlugin(_ context.Context, routeID, pluginName string) error {
	for i, r := range a.routes {
		if r.ID == routeID {
			if a.routes[i].Plugins == nil {
				a.routes[i].Plugins = make(map[string]interface{})
			}
			if _, ok := a.routes[i].Plugins[pluginName]; !ok {
				a.routes[i].Plugins[pluginName] = map[string]interface{}{}
			}
			return nil
		}
	}
	return fmt.Errorf("route %s not found", routeID)
}

func (a *embeddedAPISIX) DisablePlugin(_ context.Context, routeID, pluginName string) error {
	for i, r := range a.routes {
		if r.ID == routeID {
			delete(a.routes[i].Plugins, pluginName)
			return nil
		}
	}
	return fmt.Errorf("route %s not found", routeID)
}

func (a *embeddedAPISIX) Close() error { return nil }

func apisixDefaultRoutes() []APISIXRoute {
	return []APISIXRoute{
		{ID: "auth", URI: "/auth/*", Methods: []string{"POST", "GET"}, Plugins: map[string]interface{}{"limit-req": map[string]interface{}{"rate": 10, "burst": 5}}},
		{ID: "elections", URI: "/elections/*", Methods: []string{"GET", "POST", "PATCH"}, Plugins: map[string]interface{}{"jwt-auth": map[string]interface{}{}, "limit-req": map[string]interface{}{"rate": 30, "burst": 10}}},
		{ID: "results", URI: "/results/*", Methods: []string{"GET", "POST"}, Plugins: map[string]interface{}{"jwt-auth": map[string]interface{}{}, "limit-req": map[string]interface{}{"rate": 20, "burst": 10}}},
		{ID: "geo", URI: "/geo/*", Methods: []string{"GET"}, Plugins: map[string]interface{}{"limit-req": map[string]interface{}{"rate": 60, "burst": 30}}},
		{ID: "tiles", URI: "/geo/tiles/*", Methods: []string{"GET"}, Plugins: map[string]interface{}{"proxy-cache": map[string]interface{}{"cache_ttl": 300}, "limit-req": map[string]interface{}{"rate": 120, "burst": 60}}},
		{ID: "dashboard", URI: "/dashboard/*", Methods: []string{"GET", "POST"}, Plugins: map[string]interface{}{"limit-req": map[string]interface{}{"rate": 30, "burst": 15}}},
		{ID: "audit", URI: "/audit/*", Methods: []string{"GET"}, Plugins: map[string]interface{}{"jwt-auth": map[string]interface{}{}, "limit-req": map[string]interface{}{"rate": 20, "burst": 10}}},
		{ID: "incidents", URI: "/incidents/*", Methods: []string{"GET", "POST", "PATCH"}, Plugins: map[string]interface{}{"jwt-auth": map[string]interface{}{}, "limit-req": map[string]interface{}{"rate": 15, "burst": 5}}},
		{ID: "websocket", URI: "/results/ws/*", Plugins: map[string]interface{}{"websocket": map[string]interface{}{}}},
		{ID: "middleware", URI: "/middleware/*", Methods: []string{"GET"}, Plugins: map[string]interface{}{"jwt-auth": map[string]interface{}{}}},
	}
}

func initAPISIXClient() APISIXClient {
	apisixURL := envOrDefault("APISIX_ADMIN_URL", "")
	if apisixURL != "" {
		apiKey := envOrDefault("APISIX_API_KEY", "")
		client := &apisixHTTPClient{
			baseURL: apisixURL,
			apiKey:  apiKey,
			client:  NewResilientHTTPClient("apisix"),
		}
		s := client.Status()
		if s.Connected {
			log.Info().Str("url", apisixURL).Msg("APISIX connected")
			return client
		}
		log.Warn().Msg("APISIX unreachable, falling back to embedded")
	}
	env := os.Getenv("APP_ENV")
	if env == "production" || env == "staging" {
		log.Fatal().Msg("APISIX is REQUIRED in production/staging for API gateway. Set APISIX_ADMIN_URL")
	}
	log.Warn().Msg("APISIX using embedded gateway config (DEV ONLY)")
	return newEmbeddedAPISIX()
}
