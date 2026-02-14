package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

var aiServiceURL string

func initAIProxy() {
	aiServiceURL = os.Getenv("AI_SERVICE_URL")
	if aiServiceURL == "" {
		aiServiceURL = "http://127.0.0.1:8090"
	}
}

func proxyToAI(w http.ResponseWriter, r *http.Request, path string) {
	client := &http.Client{Timeout: 30 * time.Second}
	url := aiServiceURL + path
	if r.URL.RawQuery != "" {
		url += "?" + r.URL.RawQuery
	}
	resp, err := client.Get(url)
	if err != nil {
		writeJSON(w, 200, M{"error": "AI service unavailable", "fallback": true})
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func handleAIAnomalies(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 1)
	severity := queryParam(r, "severity", "")
	path := fmt.Sprintf("/analytics/%d/anomalies", electionID)
	if severity != "" {
		path += "?severity=" + severity
	}
	proxyToAI(w, r, path)
}

func handleAIBenford(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 1)
	proxyToAI(w, r, fmt.Sprintf("/analytics/%d/benford", electionID))
}

func handleAIIntegrity(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 1)
	proxyToAI(w, r, fmt.Sprintf("/analytics/%d/integrity_score", electionID))
}

func handleAIMethods(w http.ResponseWriter, r *http.Request) {
	proxyToAI(w, r, "/ai/methods")
}

func handleAIFallbackAnomalies(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 1)

	rows, err := db.Query(`SELECT r.polling_unit_code, pu.name, pu.registered_voters,
		COALESCE(SUM(rv.votes),0) as total_votes, r.rejected_votes
		FROM results r
		JOIN polling_units pu ON r.polling_unit_code=pu.code
		LEFT JOIN result_votes rv ON rv.result_id=r.id
		WHERE r.election_id=?
		GROUP BY r.id`, electionID)
	if err != nil {
		writeJSON(w, 200, M{"anomalies": []M{}, "summary": M{"total_analyzed": 0}, "fallback": true})
		return
	}
	defer rows.Close()

	type puData struct {
		code, name string
		registered, votes, rejected int
		turnout float64
	}
	var data []puData
	for rows.Next() {
		var d puData
		rows.Scan(&d.code, &d.name, &d.registered, &d.votes, &d.rejected)
		if d.registered > 0 {
			d.turnout = float64(d.votes+d.rejected) / float64(d.registered) * 100
		}
		data = append(data, d)
	}

	anomalies := []M{}
	for _, d := range data {
		if d.registered > 0 && d.votes > d.registered {
			anomalies = append(anomalies, M{
				"polling_unit_code": d.code, "pu_name": d.name,
				"anomaly_type": "overvoting", "severity": "critical",
				"score": d.turnout, "total_votes": d.votes,
				"registered_voters": d.registered,
			})
		}
	}

	writeJSON(w, 200, M{
		"anomalies": anomalies, "total_analyzed": len(data),
		"total_anomalies": len(anomalies), "fallback": true,
		"summary": M{"critical": len(anomalies)},
	})
}

func handleAIProxy(w http.ResponseWriter, r *http.Request) {
	electionID := queryParamInt(r, "election_id", 1)
	severity := queryParam(r, "severity", "")
	path := fmt.Sprintf("/analytics/%d/anomalies", electionID)
	if severity != "" {
		path += "?severity=" + severity
	}

	client := &http.Client{Timeout: 10 * time.Second}
	url := aiServiceURL + path
	resp, err := client.Get(url)
	if err != nil {
		handleAIFallbackAnomalies(w, r)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result M
	if json.Unmarshal(body, &result) != nil {
		handleAIFallbackAnomalies(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(body)
}
