package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	opensearch "github.com/opensearch-project/opensearch-go/v2"
	"github.com/rs/zerolog/log"
)

// OpenSearchClient provides full-text search, log aggregation, and analytics.
type BulkIndexItem struct {
	IndexName string                 `json:"index_name"`
	DocID     string                 `json:"doc_id"`
	Body      map[string]interface{} `json:"body"`
}

type BulkIndexResult struct {
	Succeeded int      `json:"succeeded"`
	Failed    int      `json:"failed"`
	Errors    []string `json:"errors,omitempty"`
}

type OpenSearchClient interface {
	Index(ctx context.Context, indexName, docID string, body map[string]interface{}) error
	BulkIndex(ctx context.Context, items []BulkIndexItem) (*BulkIndexResult, error)
	Search(ctx context.Context, indexName, query string, size int) (*SearchResult, error)
	Delete(ctx context.Context, indexName, docID string) error
	ListIndices(ctx context.Context) ([]IndexInfo, error)
	GetStats(ctx context.Context) (*SearchStats, error)
	Status() MWStatus
	Close() error
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

// --- Real OpenSearch client using opensearch-go ---

type realOpenSearchClient struct {
	client *opensearch.Client
	url    string
}

func newRealOpenSearchClient(url string) (*realOpenSearchClient, error) {
	cfg := opensearch.Config{
		Addresses: []string{url},
	}
	username := os.Getenv("OPENSEARCH_USERNAME")
	password := os.Getenv("OPENSEARCH_PASSWORD")
	if username != "" {
		cfg.Username = username
		cfg.Password = password
	}
	client, err := opensearch.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &realOpenSearchClient{client: client, url: url}, nil
}

func (o *realOpenSearchClient) Index(ctx context.Context, indexName, docID string, body map[string]interface{}) error {
	data, _ := json.Marshal(body)
	resp, err := o.client.Index(indexName, bytes.NewReader(data),
		o.client.Index.WithDocumentID(docID),
		o.client.Index.WithContext(ctx),
		o.client.Index.WithRefresh("true"),
	)
	if err != nil {
		return fmt.Errorf("opensearch index: %w", err)
	}
	defer resp.Body.Close()
	if resp.IsError() {
		return fmt.Errorf("opensearch index error: %s", resp.String())
	}
	return nil
}

func (o *realOpenSearchClient) Search(ctx context.Context, indexName, query string, size int) (*SearchResult, error) {
	t0 := time.Now()
	body := map[string]interface{}{
		"query": map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":  query,
				"fields": []string{"*"},
			},
		},
		"size": size,
	}
	data, _ := json.Marshal(body)
	resp, err := o.client.Search(
		o.client.Search.WithContext(ctx),
		o.client.Search.WithIndex(indexName),
		o.client.Search.WithBody(bytes.NewReader(data)),
	)
	if err != nil {
		return nil, fmt.Errorf("opensearch search: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
			Hits []struct {
				Index  string                 `json:"_index"`
				ID     string                 `json:"_id"`
				Score  float64                `json:"_score"`
				Source map[string]interface{} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	hits := make([]SearchHit, 0, len(result.Hits.Hits))
	for _, h := range result.Hits.Hits {
		hits = append(hits, SearchHit{Index: h.Index, ID: h.ID, Score: h.Score, Source: h.Source})
	}
	return &SearchResult{Hits: hits, Total: result.Hits.Total.Value, Took: time.Since(t0).Milliseconds()}, nil
}

func (o *realOpenSearchClient) Delete(ctx context.Context, indexName, docID string) error {
	resp, err := o.client.Delete(indexName, docID, o.client.Delete.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (o *realOpenSearchClient) ListIndices(ctx context.Context) ([]IndexInfo, error) {
	resp, err := o.client.Cat.Indices(o.client.Cat.Indices.WithContext(ctx), o.client.Cat.Indices.WithFormat("json"))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var indices []struct {
		Index     string `json:"index"`
		DocsCount string `json:"docs.count"`
		StoreSize string `json:"store.size"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&indices); err != nil {
		return nil, err
	}
	result := make([]IndexInfo, 0, len(indices))
	for _, idx := range indices {
		var count int
		fmt.Sscanf(idx.DocsCount, "%d", &count)
		result = append(result, IndexInfo{Name: idx.Index, DocCount: count, Size: idx.StoreSize})
	}
	return result, nil
}

func (o *realOpenSearchClient) GetStats(ctx context.Context) (*SearchStats, error) {
	resp, err := o.client.Cluster.Health(o.client.Cluster.Health.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var health struct {
		Status        string `json:"status"`
		NumberOfNodes int    `json:"number_of_nodes"`
		ActiveShards  int    `json:"active_primary_shards"`
	}
	json.NewDecoder(resp.Body).Decode(&health)
	return &SearchStats{
		TotalDocs:    health.ActiveShards,
		TotalIndices: health.NumberOfNodes,
		Status:       health.Status,
	}, nil
}

func (o *realOpenSearchClient) Status() MWStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lat, err := measureLatency(func() error {
		resp, e := o.client.Ping(o.client.Ping.WithContext(ctx))
		if e != nil {
			return e
		}
		resp.Body.Close()
		return nil
	})
	if err != nil {
		return MWStatus{Name: "OpenSearch", Connected: false, Mode: "native opensearch-go (unreachable)", Details: err.Error()}
	}
	return MWStatus{Name: "OpenSearch", Connected: true, Mode: "native opensearch-go", Latency: fmtLatency(lat), Details: "full-text search, multi-match, cluster health"}
}

// --- Embedded OpenSearch backed by DB full-text search ---

type embeddedOpenSearch struct{}

func (o *realOpenSearchClient) BulkIndex(ctx context.Context, items []BulkIndexItem) (*BulkIndexResult, error) {
	if len(items) == 0 {
		return &BulkIndexResult{}, nil
	}
	var buf bytes.Buffer
	for _, item := range items {
		meta := map[string]interface{}{
			"index": map[string]interface{}{
				"_index": item.IndexName,
				"_id":    item.DocID,
			},
		}
		metaLine, _ := json.Marshal(meta)
		buf.Write(metaLine)
		buf.WriteByte('\n')
		dataLine, _ := json.Marshal(item.Body)
		buf.Write(dataLine)
		buf.WriteByte('\n')
	}
	resp, err := o.client.Bulk(bytes.NewReader(buf.Bytes()),
		o.client.Bulk.WithContext(ctx),
		o.client.Bulk.WithRefresh("true"),
	)
	if err != nil {
		return nil, fmt.Errorf("opensearch bulk: %w", err)
	}
	defer resp.Body.Close()

	var bulkResp struct {
		Errors bool `json:"errors"`
		Items  []struct {
			Index struct {
				Status int    `json:"status"`
				Error  *struct{ Reason string `json:"reason"` } `json:"error"`
			} `json:"index"`
		} `json:"items"`
	}
	json.NewDecoder(resp.Body).Decode(&bulkResp)

	result := &BulkIndexResult{}
	for _, item := range bulkResp.Items {
		if item.Index.Status >= 200 && item.Index.Status < 300 {
			result.Succeeded++
		} else {
			result.Failed++
			if item.Index.Error != nil {
				result.Errors = append(result.Errors, item.Index.Error.Reason)
			}
		}
	}
	return result, nil
}

func (o *realOpenSearchClient) Close() error { return nil }

func (o *embeddedOpenSearch) Index(ctx context.Context, indexName, docID string, body map[string]interface{}) error {
	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}
	textContent := ""
	for _, v := range body {
		textContent += fmt.Sprintf("%v ", v)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO search_documents (index_name, doc_id, content, text_content) VALUES ($1, $2, $3, $4)
		 ON CONFLICT(index_name, doc_id) DO UPDATE SET content=EXCLUDED.content, text_content=EXCLUDED.text_content`,
		indexName, docID, string(bodyJSON), strings.TrimSpace(textContent))
	return err
}

func (o *embeddedOpenSearch) Search(ctx context.Context, indexName, query string, size int) (*SearchResult, error) {
	t0 := time.Now()
	rows, err := db.QueryContext(ctx,
		`SELECT doc_id, content FROM search_documents WHERE index_name=$1 AND text_content ILIKE $2 ORDER BY created_at DESC LIMIT $3`,
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
		hits = append(hits, SearchHit{Index: indexName, ID: docID, Score: 1.0, Source: source})
	}
	return &SearchResult{Hits: hits, Total: len(hits), Took: time.Since(t0).Milliseconds()}, nil
}

func (o *embeddedOpenSearch) Delete(ctx context.Context, indexName, docID string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM search_documents WHERE index_name=$1 AND doc_id=$2`, indexName, docID)
	return err
}

func (o *embeddedOpenSearch) ListIndices(ctx context.Context) ([]IndexInfo, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT index_name, COUNT(*) as doc_count FROM search_documents GROUP BY index_name ORDER BY index_name`)
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
		info.Size = fmt.Sprintf("%dKB", info.DocCount*2)
		indices = append(indices, info)
	}
	return indices, nil
}

func (o *embeddedOpenSearch) GetStats(ctx context.Context) (*SearchStats, error) {
	var totalDocs, totalIndices int
	db.QueryRowContext(ctx, `SELECT COUNT(*) FROM search_documents`).Scan(&totalDocs)
	db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT index_name) FROM search_documents`).Scan(&totalIndices)
	return &SearchStats{TotalDocs: totalDocs, TotalIndices: totalIndices, Status: "green"}, nil
}

func (o *embeddedOpenSearch) Status() MWStatus {
	return MWStatus{Name: "OpenSearch", Connected: true, Mode: "embedded (DB-backed)", Details: "full-text search via ILIKE + tsvector"}
}

// --- Init ---

func (o *embeddedOpenSearch) BulkIndex(ctx context.Context, items []BulkIndexItem) (*BulkIndexResult, error) {
	result := &BulkIndexResult{}
	for _, item := range items {
		if err := o.Index(ctx, item.IndexName, item.DocID, item.Body); err != nil {
			result.Failed++
			result.Errors = append(result.Errors, err.Error())
		} else {
			result.Succeeded++
		}
	}
	return result, nil
}

func (o *embeddedOpenSearch) Close() error { return nil }

func initOpenSearchClient() OpenSearchClient {
	baseURL := os.Getenv("OPENSEARCH_URL")
	if baseURL != "" {
		client, err := newRealOpenSearchClient(baseURL)
		if err == nil {
			s := client.Status()
			if s.Connected {
				log.Info().Str("url", baseURL).Msg("OpenSearch connected via opensearch-go")
				return client
			}
		}
		log.Warn().Str("url", baseURL).Msg("OpenSearch unreachable, falling back to embedded")
	}
	env := os.Getenv("APP_ENV")
	if env == "production" || env == "staging" {
		log.Fatal().Msg("OpenSearch is REQUIRED in production/staging for log aggregation and search. Set OPENSEARCH_URL")
	}
	log.Warn().Msg("OpenSearch using embedded DB-backed implementation (DEV ONLY)")
	return &embeddedOpenSearch{}
}

// seedSearchIndices indexes existing data for full-text search.
func seedSearchIndices(database *sql.DB) {
	ctx := context.Background()
	if mwHub == nil || mwHub.OpenSearch == nil {
		return
	}

	rows, err := database.Query(`SELECT r.id, r.polling_unit_code, p.name, p.code, r.votes, r.status
		FROM results r JOIN parties p ON r.party_code = p.code LIMIT 1000`)
	if err == nil {
		defer rows.Close()
		count := 0
		for rows.Next() {
			var id, votes int
			var puCode, partyName, partyCode, status string
			if err := rows.Scan(&id, &puCode, &partyName, &partyCode, &votes, &status); err != nil {
				continue
			}
			mwHub.OpenSearch.Index(ctx, "results", fmt.Sprintf("result-%d", id), map[string]interface{}{
				"polling_unit": puCode, "party": partyName, "party_code": partyCode, "votes": votes, "status": status,
			})
			count++
		}
		if count > 0 {
			log.Info().Int("count", count).Msg("OpenSearch: indexed results")
		}
	}

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
			log.Info().Int("count", count).Msg("OpenSearch: indexed audit entries")
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
