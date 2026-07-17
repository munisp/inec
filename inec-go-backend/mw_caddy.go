package main

import (
"bytes"
"context"
"encoding/json"
"fmt"
"net/http"
"time"
)

type CaddyClient interface {
Status() MWStatus
UpdateRoute(ctx context.Context, routeID string, routeConfig map[string]interface{}) error
Close() error
}

type caddyHTTPClient struct {
adminURL string
client   *http.Client
}

func newCaddyClient(adminURL string) CaddyClient {
if adminURL == "" {
adminURL = "http://caddy:2019"
}
return &caddyHTTPClient{
adminURL: adminURL,
client: &http.Client{
Timeout: 5 * time.Second,
},
}
}

func (c *caddyHTTPClient) Status() MWStatus {
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

req, _ := http.NewRequestWithContext(ctx, "GET", c.adminURL+"/config/", nil)

start := time.Now()
resp, err := c.client.Do(req)
lat := time.Since(start)

if err != nil {
return MWStatus{Name: "Caddy", Connected: false, Mode: "edge", Details: err.Error()}
}
defer resp.Body.Close()

if resp.StatusCode != http.StatusOK {
return MWStatus{Name: "Caddy", Connected: false, Mode: "edge", Details: fmt.Sprintf("HTTP %d", resp.StatusCode)}
}

return MWStatus{Name: "Caddy", Connected: true, Mode: "edge", Latency: fmtLatency(lat), Details: "Admin API connected"}
}

func (c *caddyHTTPClient) UpdateRoute(ctx context.Context, routeID string, routeConfig map[string]interface{}) error {
payload, err := json.Marshal(routeConfig)
if err != nil {
return err
}

req, err := http.NewRequestWithContext(ctx, "PATCH", fmt.Sprintf("%s/config/apps/http/servers/srv0/routes/%s", c.adminURL, routeID), bytes.NewReader(payload))
if err != nil {
return err
}
req.Header.Set("Content-Type", "application/json")

resp, err := c.client.Do(req)
if err != nil {
return err
}
defer resp.Body.Close()

if resp.StatusCode >= 400 {
return fmt.Errorf("caddy admin api returned %d", resp.StatusCode)
}

return nil
}

func (c *caddyHTTPClient) Close() error {
return nil
}
