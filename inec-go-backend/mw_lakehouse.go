package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type LakehouseQuery struct {
	Query      string                 `json:"query"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Format     string                 `json:"format,omitempty"`
}

type LakehouseResult struct {
	Columns []string                 `json:"columns"`
	Rows    []map[string]interface{} `json:"rows"`
	Count   int                      `json:"count"`
	QueryMs float64                  `json:"query_ms"`
}

type LakehouseClient interface {
	Query(ctx context.Context, query LakehouseQuery) (*LakehouseResult, error)
	Ingest(ctx context.Context, table string, rows []map[string]interface{}) error
	GetTables(ctx context.Context) ([]string, error)
	GetAnalytics(ctx context.Context, electionID int, analysisType string) (map[string]interface{}, error)
	Status() MWStatus
	Close() error
}

type lakehouseHTTPClient struct {
	baseURL string
	client  *http.Client
}

func (l *lakehouseHTTPClient) Query(ctx context.Context, query LakehouseQuery) (*LakehouseResult, error) {
	body, _ := json.Marshal(query)
	req, _ := http.NewRequestWithContext(ctx, "POST", l.baseURL+"/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result LakehouseResult
	json.NewDecoder(resp.Body).Decode(&result)
	return &result, nil
}

func (l *lakehouseHTTPClient) Ingest(ctx context.Context, table string, rows []map[string]interface{}) error {
	body, _ := json.Marshal(map[string]interface{}{"table": table, "rows": rows})
	req, _ := http.NewRequestWithContext(ctx, "POST", l.baseURL+"/ingest", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (l *lakehouseHTTPClient) GetTables(ctx context.Context) ([]string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", l.baseURL+"/tables", nil)
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tables []string
	json.NewDecoder(resp.Body).Decode(&tables)
	return tables, nil
}

func (l *lakehouseHTTPClient) GetAnalytics(ctx context.Context, electionID int, analysisType string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/analytics/%d/%s", l.baseURL, electionID, analysisType)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	return result, nil
}

func (l *lakehouseHTTPClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", l.baseURL+"/health", nil)
	lat, err := measureLatency(func() error {
		resp, e := l.client.Do(req)
		if e != nil {
			return e
		}
		resp.Body.Close()
		return nil
	})
	if err != nil {
		return MWStatus{Name: "Lakehouse", Connected: false, Mode: "external (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "Lakehouse", Connected: true, Mode: "external", Latency: fmtLatency(lat)}
}

func (l *lakehouseHTTPClient) Close() error { return nil }

type embeddedLakehouse struct{}

func (l *embeddedLakehouse) Query(_ context.Context, query LakehouseQuery) (*LakehouseResult, error) {
	t0 := time.Now()
	rows := make([]map[string]interface{}, 0)
	result := &LakehouseResult{
		Columns: []string{"info"},
		Rows:    rows,
		Count:   0,
		QueryMs: float64(time.Since(t0).Microseconds()) / 1000.0,
	}

	dbRows, _ := db.Query("SELECT r.id, r.election_id, r.polling_unit_code, r.status, r.submitted_at FROM results r LIMIT 100")
	allResults := scanRows(dbRows)
	for _, r := range allResults {
		rows = append(rows, r)
	}
	result.Rows = rows
	result.Count = len(rows)
	result.QueryMs = float64(time.Since(t0).Microseconds()) / 1000.0
	return result, nil
}

func (l *embeddedLakehouse) Ingest(_ context.Context, _ string, _ []map[string]interface{}) error {
	return nil
}

func (l *embeddedLakehouse) GetTables(_ context.Context) ([]string, error) {
	return []string{"results", "elections", "polling_units", "audit_log", "incidents", "collation_results"}, nil
}

func (l *embeddedLakehouse) GetAnalytics(_ context.Context, electionID int, analysisType string) (map[string]interface{}, error) {
	t0 := time.Now()
	result := map[string]interface{}{
		"election_id": electionID,
		"type":        analysisType,
		"generated_at": time.Now().UTC().Format(time.RFC3339),
	}

	switch analysisType {
	case "turnout":
		rows, _ := db.Query(`SELECT s.name as state_name, s.code as state_code,
			COUNT(r.id) as result_count,
			SUM(r.total_votes) as total_votes,
			SUM(r.registered_voters) as registered_voters
			FROM results r JOIN polling_units pu ON r.polling_unit_id = pu.id
			JOIN wards w ON pu.ward_id = w.id JOIN lgas l ON w.lga_id = l.id
			JOIN states s ON l.state_id = s.id
			WHERE r.election_id = ? GROUP BY s.id ORDER BY s.name`, electionID)
		if rows != nil {
			result["states"] = scanRows(rows)
		}
	case "party_performance":
		rows, _ := db.Query(`SELECT r.party_scores FROM results r WHERE r.election_id = ? AND r.status IN ('validated','finalized')`, electionID)
		if rows != nil {
			partyTotals := make(map[string]int64)
			scanned := scanRows(rows)
			for _, row := range scanned {
				if ps, ok := row["party_scores"].(string); ok {
					var scores map[string]interface{}
					json.Unmarshal([]byte(ps), &scores)
					for party, v := range scores {
						if n, ok := v.(float64); ok {
							partyTotals[party] += int64(n)
						}
					}
				}
			}
			result["party_totals"] = partyTotals
		}
	case "timeline":
		rows, _ := db.Query(`SELECT DATE(r.submitted_at) as date, COUNT(*) as count,
			SUM(r.total_votes) as total_votes
			FROM results r WHERE r.election_id = ? GROUP BY DATE(r.submitted_at) ORDER BY date`, electionID)
		if rows != nil {
			result["timeline"] = scanRows(rows)
		}
	case "anomalies":
		rows, _ := db.Query(`SELECT r.id, pu.name as pu_name, r.total_votes, r.registered_voters,
			CAST(r.total_votes AS FLOAT) / NULLIF(r.registered_voters, 0) as turnout_pct
			FROM results r JOIN polling_units pu ON r.polling_unit_id = pu.id
			WHERE r.election_id = ? AND CAST(r.total_votes AS FLOAT) / NULLIF(r.registered_voters, 0) > 0.95
			ORDER BY turnout_pct DESC LIMIT 50`, electionID)
		if rows != nil {
			result["anomalies"] = scanRows(rows)
		}
	default:
		result["error"] = "unknown analysis type"
	}

	result["query_ms"] = float64(time.Since(t0).Microseconds()) / 1000.0
	return result, nil
}

func (l *embeddedLakehouse) Status() MWStatus {
	return MWStatus{
		Name: "Lakehouse", Connected: true, Mode: "embedded",
		Latency: "0.0ms",
		Details: "SQLite-backed analytics (upgrade to DuckDB/Delta Lake for production)",
	}
}

func (l *embeddedLakehouse) Close() error { return nil }

func initLakehouseClient() LakehouseClient {
	lakehouseURL := envOrDefault("LAKEHOUSE_URL", "")
	if lakehouseURL != "" {
		client := &lakehouseHTTPClient{
			baseURL: lakehouseURL,
			client:  &http.Client{Timeout: 30 * time.Second},
		}
		s := client.Status()
		if s.Connected {
			log.Println("[Lakehouse] Connected to external Lakehouse at", lakehouseURL)
			return client
		}
		log.Println("[Lakehouse] External Lakehouse unreachable, falling back to embedded")
	}
	log.Println("[Lakehouse] Using embedded SQLite analytics")
	return &embeddedLakehouse{}
}
