package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

type PermifyCheck struct {
	Subject    string `json:"subject"`
	SubjectType string `json:"subject_type"`
	Permission string `json:"permission"`
	Resource   string `json:"resource"`
	ResourceType string `json:"resource_type"`
}

type PermifyClient interface {
	Check(ctx context.Context, check PermifyCheck) (bool, error)
	WriteRelationship(ctx context.Context, subject, subjectType, relation, resource, resourceType string) error
	DeleteRelationship(ctx context.Context, subject, subjectType, relation, resource, resourceType string) error
	LookupResources(ctx context.Context, subjectType, subjectID, permission, resourceType string) ([]string, error)
	Status() MWStatus
	Close() error
}

type permifyHTTPClient struct {
	baseURL  string
	tenantID string
	client   *http.Client
}

func (p *permifyHTTPClient) Check(ctx context.Context, check PermifyCheck) (bool, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{"depth": 5},
		"entity":   map[string]string{"type": check.ResourceType, "id": check.Resource},
		"permission": check.Permission,
		"subject":    map[string]interface{}{"type": check.SubjectType, "id": check.Subject, "relation": ""},
	})
	url := fmt.Sprintf("%s/v1/tenants/%s/permissions/check", p.baseURL, p.tenantID)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	var result struct {
		Can string `json:"can"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Can == "CHECK_RESULT_ALLOWED", nil
}

func (p *permifyHTTPClient) WriteRelationship(ctx context.Context, subject, subjectType, relation, resource, resourceType string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"metadata": map[string]string{},
		"tuples": []map[string]interface{}{{
			"entity":   map[string]string{"type": resourceType, "id": resource},
			"relation": relation,
			"subject":  map[string]interface{}{"type": subjectType, "id": subject, "relation": ""},
		}},
	})
	url := fmt.Sprintf("%s/v1/tenants/%s/relationships/write", p.baseURL, p.tenantID)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (p *permifyHTTPClient) DeleteRelationship(ctx context.Context, subject, subjectType, relation, resource, resourceType string) error {
	body, _ := json.Marshal(map[string]interface{}{
		"entity":   map[string]string{"type": resourceType, "id": resource},
		"relation": relation,
		"subject":  map[string]interface{}{"type": subjectType, "id": subject, "relation": ""},
	})
	url := fmt.Sprintf("%s/v1/tenants/%s/relationships/delete", p.baseURL, p.tenantID)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (p *permifyHTTPClient) LookupResources(ctx context.Context, subjectType, subjectID, permission, resourceType string) ([]string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"metadata":      map[string]interface{}{"depth": 5},
		"entity_type":   resourceType,
		"permission":    permission,
		"subject":       map[string]interface{}{"type": subjectType, "id": subjectID, "relation": ""},
	})
	url := fmt.Sprintf("%s/v1/tenants/%s/permissions/lookup-entity", p.baseURL, p.tenantID)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		EntityIDs []string `json:"entity_ids"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.EntityIDs, nil
}

func (p *permifyHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/healthz", nil)
	lat, err := measureLatency(func() error {
		resp, e := p.client.Do(req)
		if e != nil {
			return e
		}
		resp.Body.Close()
		return nil
	})
	if err != nil {
		return MWStatus{Name: "Permify", Connected: false, Mode: "external (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "Permify", Connected: true, Mode: "external", Latency: fmtLatency(lat)}
}

func (p *permifyHTTPClient) Close() error { return nil }

var permifyRBAC = map[string]map[string]bool{
	"admin":              {"submit_result": true, "validate_result": true, "finalize_result": true, "dispute_result": true, "create_election": true, "update_election": true, "view_audit": true, "manage_incidents": true, "export_data": true, "view_dashboard": true},
	"presiding_officer":  {"submit_result": true, "view_dashboard": true},
	"collation_officer":  {"validate_result": true, "finalize_result": true, "view_dashboard": true, "view_audit": true},
	"observer":           {"dispute_result": true, "view_audit": true, "view_dashboard": true},
	"public":             {"view_dashboard": true},
}

type embeddedPermify struct{}

func (p *embeddedPermify) Check(_ context.Context, check PermifyCheck) (bool, error) {
	if perms, ok := permifyRBAC[check.SubjectType]; ok {
		return perms[check.Permission], nil
	}
	if perms, ok := permifyRBAC[check.Subject]; ok {
		return perms[check.Permission], nil
	}
	return false, nil
}

func (p *embeddedPermify) WriteRelationship(_ context.Context, _, _, _, _, _ string) error {
	return nil
}

func (p *embeddedPermify) DeleteRelationship(_ context.Context, _, _, _, _, _ string) error {
	return nil
}

func (p *embeddedPermify) LookupResources(_ context.Context, subjectType, _, permission, _ string) ([]string, error) {
	if perms, ok := permifyRBAC[subjectType]; ok {
		if perms[permission] {
			return []string{"*"}, nil
		}
	}
	return nil, nil
}

func (p *embeddedPermify) Status() MWStatus {
	return MWStatus{
		Name: "Permify", Connected: true, Mode: "embedded",
		Latency: "0.0ms",
		Details: fmt.Sprintf("local RBAC with %d roles", len(permifyRBAC)),
	}
}

func (p *embeddedPermify) Close() error { return nil }

func initPermifyClient() PermifyClient {
	permifyURL := envOrDefault("PERMIFY_URL", "")
	if permifyURL != "" {
		tenantID := envOrDefault("PERMIFY_TENANT_ID", "inec")
		client := &permifyHTTPClient{
			baseURL:  permifyURL,
			tenantID: tenantID,
			client:   &http.Client{Timeout: 5 * time.Second},
		}
		s := client.Status()
		if s.Connected {
			log.Info().Str("url", permifyURL).Msg("Permify connected via HTTP")
			return client
		}
		log.Warn().Str("url", permifyURL).Msg("Permify unreachable, falling back to local RBAC")
	}
	log.Info().Msg("Permify using embedded local RBAC")
	return &embeddedPermify{}
}
