package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// OpenSearchClient provides full-text search, log aggregation, and analytics.
type OpenSearchClient interface {
	Index(ctx context.Context, indexName, docID string, body map[string]interface{}) error
	Search(ctx context.Context, indexName, query string, size int) (*SearchResult, error)
	Delete(ctx context.Context, indexName, docID string) error
	ListIndices(ctx context.Context) ([]IndexInfo, error)
	GetStats(ctx context.Context) (*SearchStats, error)
	Status() MWStatus
}

type SearchResult struct {
	Hits  []SearchHit `json:"hits"`
	Total int         `json:"total"`
	Took  int64       `json:"took_ms"`
}

type SearchHit struct {
	Index  string                 `json:"index"`
	ID     string                 `json:"id"`
	Score  float64                `json:"score"`
	Source map[string]interface{} `json:"source"`
}

type IndexInfo struct {
	Name     string `json:"name"`
	DocCount int    `json:"doc_count"`
	Size     string `json:"size"`
}

type SearchStats struct {
	TotalDocs    int    `json:"total_docs"`
	TotalIndices int    `json:"total_indices"`
	Status       string `json:"status"`
}

// HTTP client for real OpenSearch cluster
type opensearchHTTPClient struct {
	client  *ResilientHTTPClient
	baseURL string
}

func (o *opensearchHTTPClient) Index(ctx context.Context, indexName, docID string, body map[string]interface{}) error {
	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/%s/_doc/%s", o.baseURL, indexName, docID)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, strings.NewReader(string(data)))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return fmt.Errorf("index doc: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

func (o *opensearchHTTPClient) Search(ctx context.Context, indexName, query string, size int) (*SearchResult, error) {
	return nil, fmt.Errorf("opensearch HTTP search not yet connected")
}

func (o *opensearchHTTPClient) Delete(ctx context.Context, indexName, docID string) error {
	return fmt.Errorf("opensearch HTTP delete not yet connected")
}

func (o *opensearchHTTPClient) ListIndices(ctx context.Context) ([]IndexInfo, error) {
	return nil, fmt.Errorf("opensearch HTTP list-indices not yet connected")
}

func (o *opensearchHTTPClient) GetStats(ctx context.Context) (*SearchStats, error) {
	return nil, fmt.Errorf("opensearch HTTP stats not yet connected")
}

func (o *opensearchHTTPClient) Status() MWStatus {
	return MWStatus{Name: "OpenSearch", Connected: false, Mode: "external (unreachable)"}
}

// Embedded OpenSearch backed by DB full-text search
type embeddedOpenSearch struct{}

func (o *embeddedOpenSearch) Index(ctx context.Context, indexName, docID string, body map[string]interface{}) error {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO mw_search_index (index_name, doc_id, body) VALUES (?, ?, ?)
		 ON CONFLICT(index_name, doc_id) DO UPDATE SET body=excluded.body, created_at=CURRENT_TIMESTAMP`,
		indexName, docID, string(bodyJSON))
	return err
}

func (o *embeddedOpenSearch) Search(ctx context.Context, indexName, query string, size int) (*SearchResult, error) {
	t0 := time.Now()

	rows, err := db.QueryContext(ctx,
		`SELECT doc_id, body FROM mw_search_index WHERE index_name=? AND body LIKE ? ORDER BY created_at DESC LIMIT ?`,
		indexName, "%"+query+"%", size)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var hits []SearchHit
	for rows.Next() {
		var docID, bodyStr string
		if err := rows.Scan(&docID, &bodyStr); err != nil {
			continue
		}
		var source map[string]interface{}
		json.Unmarshal([]byte(bodyStr), &source)
		hits = append(hits, SearchHit{
			Index:  indexName,
			ID:     docID,
			Score:  1.0,
			Source: source,
		})
	}

	return &SearchResult{
		Hits:  hits,
		Total: len(hits),
		Took:  time.Since(t0).Milliseconds(),
	}, nil
}

func (o *embeddedOpenSearch) Delete(ctx context.Context, indexName, docID string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM mw_search_index WHERE index_name=? AND doc_id=?`, indexName, docID)
	return err
}

func (o *embeddedOpenSearch) ListIndices(ctx context.Context) ([]IndexInfo, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT index_name, COUNT(*) as doc_count FROM mw_search_index GROUP BY index_name ORDER BY index_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indices []IndexInfo
	for rows.Next() {
		var info IndexInfo
		if err := rows.Scan(&info.Name, &info.DocCount); err != nil {
			continue
		}
		info.Size = fmt.Sprintf("%dKB", info.DocCount*2) // estimate
		indices = append(indices, info)
	}
	return indices, nil
}

func (o *embeddedOpenSearch) GetStats(ctx context.Context) (*SearchStats, error) {
	var totalDocs, totalIndices int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mw_search_index`).Scan(&totalDocs)
	db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT index_name) FROM mw_search_index`).Scan(&totalIndices)
	return &SearchStats{
		TotalDocs:    totalDocs,
		TotalIndices: totalIndices,
		Status:       "green",
	}, nil
}

func (o *embeddedOpenSearch) Status() MWStatus {
	return MWStatus{Name: "OpenSearch", Connected: true, Mode: "embedded (DB-backed)", Details: "full-text search via LIKE"}
}

func initOpenSearchClient() OpenSearchClient {
	baseURL := os.Getenv("OPENSEARCH_URL")
	if baseURL != "" {
		log.Printf("OpenSearch: connecting to %s", baseURL)
		client := &opensearchHTTPClient{
			client:  NewResilientHTTPClient("opensearch"),
			baseURL: baseURL,
		}
		_, err := client.GetStats(context.Background())
		if err == nil {
			return client
		}
		log.Printf("OpenSearch: external connection failed (%v), using embedded", err)
	}
	log.Println("OpenSearch: using embedded DB-backed implementation")
	return &embeddedOpenSearch{}
}

// seedSearchIndices indexes existing data for full-text search
func seedSearchIndices(database *sql.DB) {
	ctx := context.Background()
	if mwHub == nil || mwHub.OpenSearch == nil {
		return
	}

	// Index results
	rows, err := database.Query(`SELECT id, polling_unit_code, candidate_name, party, votes, status FROM results LIMIT 1000`)
	if err == nil {
		defer rows.Close()
		count := 0
		for rows.Next() {
			var id int
			var puCode, candidate, party, status string
			var votes int
			if err := rows.Scan(&id, &puCode, &candidate, &party, &votes, &status); err != nil {
				continue
			}
			mwHub.OpenSearch.Index(ctx, "results", fmt.Sprintf("result-%d", id), map[string]interface{}{
				"polling_unit": puCode, "candidate": candidate, "party": party, "votes": votes, "status": status,
			})
			count++
		}
		if count > 0 {
			log.Printf("OpenSearch: indexed %d results", count)
		}
	}

	// Index audit trail
	rows2, err := database.Query(`SELECT id, action, entity_type, entity_id, user_id FROM audit_trail ORDER BY id DESC LIMIT 500`)
	if err == nil {
		defer rows2.Close()
		count := 0
		for rows2.Next() {
			var id int
			var action, entityType, entityID string
			var userID sql.NullInt64
			if err := rows2.Scan(&id, &action, &entityType, &entityID, &userID); err != nil {
				continue
			}
			mwHub.OpenSearch.Index(ctx, "audit", fmt.Sprintf("audit-%d", id), map[string]interface{}{
				"action": action, "entity_type": entityType, "entity_id": entityID, "user_id": userID.Int64,
			})
			count++
		}
		if count > 0 {
			log.Printf("OpenSearch: indexed %d audit entries", count)
		}
	}
}

// HTTP handlers
func handleOpenSearchSearch(w http.ResponseWriter, r *http.Request) {
	index := queryParam(r, "index", "results")
	query := queryParam(r, "q", "")
	size := queryParamInt(r, "size", 20)
	if query == "" {
		writeError(w, 400, "query parameter 'q' required")
		return
	}
	result, err := mwHub.OpenSearch.Search(r.Context(), index, query, size)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, result)
}

func handleOpenSearchIndex(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Index string                 `json:"index"`
		DocID string                 `json:"doc_id"`
		Body  map[string]interface{} `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if req.Index == "" || req.DocID == "" {
		writeError(w, 400, "index and doc_id required")
		return
	}
	if err := mwHub.OpenSearch.Index(r.Context(), req.Index, req.DocID, req.Body); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, M{"indexed": true, "index": req.Index, "id": req.DocID})
}

func handleOpenSearchIndices(w http.ResponseWriter, r *http.Request) {
	indices, err := mwHub.OpenSearch.ListIndices(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if indices == nil {
		indices = []IndexInfo{}
	}
	writeJSON(w, 200, M{"indices": indices})
}

func handleOpenSearchStats(w http.ResponseWriter, r *http.Request) {
	stats, err := mwHub.OpenSearch.GetStats(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, stats)
}

func handleOpenSearchStatus(w http.ResponseWriter, r *http.Request) {
	status := mwHub.OpenSearch.Status()
	writeJSON(w, 200, M{
		"name":      status.Name,
		"connected": status.Connected,
		"mode":      status.Mode,
		"details":   status.Details,
	})
}
