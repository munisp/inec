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
	Subject      string `json:"subject"`
	SubjectType  string `json:"subject_type"`
	Permission   string `json:"permission"`
	Resource     string `json:"resource"`
	ResourceType string `json:"resource_type"`
}

type BulkPermifyCheck struct {
	Checks []PermifyCheck `json:"checks"`
}

type BulkPermifyResult struct {
	Results []bool `json:"results"`
	Allowed int    `json:"allowed"`
	Denied  int    `json:"denied"`
}

type PermifyClient interface {
	Check(ctx context.Context, check PermifyCheck) (bool, error)
	BulkCheck(ctx context.Context, checks []PermifyCheck) (*BulkPermifyResult, error)
	WriteRelationship(ctx context.Context, subject, subjectType, relation, resource, resourceType string) error
	DeleteRelationship(ctx context.Context, subject, subjectType, relation, resource, resourceType string) error
	LookupResources(ctx context.Context, subjectType, subjectID, permission, resourceType string) ([]string, error)
	Status() MWStatus
	Close() error
}

type permifyHTTPClient struct {
	baseURL  string
	tenantID string
	client   *ResilientHTTPClient
}

func (p *permifyHTTPClient) Check(ctx context.Context, check PermifyCheck) (bool, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"metadata":   map[string]interface{}{"depth": 5},
		"entity":     map[string]string{"type": check.ResourceType, "id": check.Resource},
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
		"metadata":    map[string]interface{}{"depth": 5},
		"entity_type": resourceType,
		"permission":  permission,
		"subject":     map[string]interface{}{"type": subjectType, "id": subjectID, "relation": ""},
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
		resp, e := p.client.Client.Do(req)
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

func (p *permifyHTTPClient) BulkCheck(ctx context.Context, checks []PermifyCheck) (*BulkPermifyResult, error) {
	result := &BulkPermifyResult{Results: make([]bool, len(checks))}
	for i, check := range checks {
		allowed, err := p.Check(ctx, check)
		if err != nil {
			return nil, fmt.Errorf("bulk check item %d: %w", i, err)
		}
		result.Results[i] = allowed
		if allowed {
			result.Allowed++
		} else {
			result.Denied++
		}
	}
	return result, nil
}

func (p *permifyHTTPClient) Close() error { return nil }

// Zanzibar-style permission model: entity#relation@subject
// Schema: election#admin@user:alice, polling_unit#presiding_officer@user:bob
var permifyRBAC = map[string]map[string]bool{
	"admin":             {"submit_result": true, "validate_result": true, "finalize_result": true, "dispute_result": true, "create_election": true, "update_election": true, "view_audit": true, "manage_incidents": true, "export_data": true, "view_dashboard": true, "manage_users": true, "rotate_keys": true, "manage_bvas": true, "resolve_duplicates": true, "manage_compliance": true},
	"presiding_officer": {"submit_result": true, "view_dashboard": true, "accredit_voter": true, "manage_bvas": true, "report_incident": true},
	"collation_officer": {"validate_result": true, "finalize_result": true, "view_dashboard": true, "view_audit": true, "collate_results": true},
	"observer":          {"dispute_result": true, "view_audit": true, "view_dashboard": true, "observe_election": true, "report_incident": true},
	"returning_officer": {"finalize_result": true, "declare_result": true, "view_dashboard": true, "view_audit": true, "collate_results": true},
	"ict_officer":       {"manage_bvas": true, "view_dashboard": true, "view_audit": true, "troubleshoot_device": true},
	"security":          {"view_dashboard": true, "view_audit": true, "manage_incidents": true, "escort_materials": true},
	"public":            {"view_dashboard": true, "view_results": true},
}

type pgPermify struct{}

func newPGPermify() *pgPermify {
	db.Exec(`CREATE TABLE IF NOT EXISTS permify_relationships (
		id SERIAL PRIMARY KEY,
		subject TEXT NOT NULL,
		subject_type TEXT NOT NULL,
		relation TEXT NOT NULL,
		resource TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(subject, subject_type, relation, resource, resource_type)
	)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_permify_subject ON permify_relationships(subject, subject_type)`)
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_permify_resource ON permify_relationships(resource, resource_type)`)
	log.Info().Int("roles", len(permifyRBAC)).Msg("Permify: PostgreSQL-backed Zanzibar model initialized")
	return &pgPermify{}
}

func (p *pgPermify) Check(_ context.Context, check PermifyCheck) (bool, error) {
	// Check direct relationship in PG
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM permify_relationships 
		WHERE subject=$1 AND subject_type=$2 AND relation=$3 AND resource=$4 AND resource_type=$5`,
		check.Subject, check.SubjectType, check.Permission, check.Resource, check.ResourceType).Scan(&count)
	if count > 0 {
		return true, nil
	}

	// Check role-based wildcard relationships
	db.QueryRow(`SELECT COUNT(*) FROM permify_relationships 
		WHERE subject=$1 AND subject_type=$2 AND relation=$3 AND resource='*' AND resource_type=$4`,
		check.Subject, check.SubjectType, check.Permission, check.ResourceType).Scan(&count)
	if count > 0 {
		return true, nil
	}

	// Fall back to static RBAC
	if perms, ok := permifyRBAC[check.SubjectType]; ok {
		return perms[check.Permission], nil
	}
	if perms, ok := permifyRBAC[check.Subject]; ok {
		return perms[check.Permission], nil
	}
	return false, nil
}

func (p *pgPermify) WriteRelationship(_ context.Context, subject, subjectType, relation, resource, resourceType string) error {
	_, err := db.Exec(`INSERT INTO permify_relationships (subject, subject_type, relation, resource, resource_type)
		VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
		subject, subjectType, relation, resource, resourceType)
	return err
}

func (p *pgPermify) DeleteRelationship(_ context.Context, subject, subjectType, relation, resource, resourceType string) error {
	_, err := db.Exec(`DELETE FROM permify_relationships 
		WHERE subject=$1 AND subject_type=$2 AND relation=$3 AND resource=$4 AND resource_type=$5`,
		subject, subjectType, relation, resource, resourceType)
	return err
}

func (p *pgPermify) LookupResources(_ context.Context, subjectType, subjectID, permission, resourceType string) ([]string, error) {
	rows, err := db.Query(`SELECT resource FROM permify_relationships 
		WHERE subject=$1 AND subject_type=$2 AND relation=$3 AND resource_type=$4`,
		subjectID, subjectType, permission, resourceType)
	if err != nil {
		// Fall back to RBAC
		if perms, ok := permifyRBAC[subjectType]; ok {
			if perms[permission] {
				return []string{"*"}, nil
			}
		}
		return nil, nil
	}
	defer rows.Close()
	var resources []string
	for rows.Next() {
		var res string
		rows.Scan(&res)
		resources = append(resources, res)
	}
	if len(resources) == 0 {
		if perms, ok := permifyRBAC[subjectType]; ok {
			if perms[permission] {
				return []string{"*"}, nil
			}
		}
	}
	return resources, nil
}


func (p *pgPermify) BulkCheck(ctx context.Context, checks []PermifyCheck) (*BulkPermifyResult, error) {
	results := make([]bool, len(checks))
	for i, c := range checks {
		ok, err := p.Check(ctx, c)
		if err != nil {
			return nil, err
		}
		results[i] = ok
	}
	return &BulkPermifyResult{Results: results}, nil
}
func (p *pgPermify) Status() MWStatus {
	var relCount int
	db.QueryRow(`SELECT COUNT(*) FROM permify_relationships`).Scan(&relCount)
	return MWStatus{
		Name: "Permify", Connected: true, Mode: "pg-zanzibar",
		Latency: "< 1ms",
		Details: fmt.Sprintf("PostgreSQL Zanzibar model, %d roles, %d relationships", len(permifyRBAC), relCount),
	}
}

func (p *pgPermify) Close() error { return nil }

func initPermifyClient() PermifyClient {
	permifyURL := envOrDefault("PERMIFY_URL", "")
	if permifyURL != "" {
		tenantID := envOrDefault("PERMIFY_TENANT_ID", "inec")
		client := &permifyHTTPClient{
			baseURL:  permifyURL,
			tenantID: tenantID,
			client:   NewResilientHTTPClient("permify"),
		}
		s := client.Status()
		if s.Connected {
			log.Info().Str("url", permifyURL).Msg("Permify connected via HTTP")
			return client
		}
		log.Warn().Str("url", permifyURL).Msg("Permify unreachable, falling back to local RBAC")
	}
	log.Info().Msg("Permify using PostgreSQL-backed Zanzibar model")
	return newPGPermify()
}
