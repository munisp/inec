package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const base = "http://localhost:8088"

type LoadTestResult struct {
	Name           string        `json:"name"`
	TotalRequests  int64         `json:"total_requests"`
	Successful     int64         `json:"successful"`
	Failed         int64         `json:"failed"`
	Duration       time.Duration `json:"duration"`
	RPS            float64       `json:"rps"`
	AvgLatency     time.Duration `json:"avg_latency_ms"`
	P99Latency     time.Duration `json:"p99_latency_ms"`
	MaxLatency     time.Duration `json:"max_latency_ms"`
	StatusCodes    map[int]int64 `json:"status_codes"`
}

func getToken() string {
	body := `{"username":"admin","password":"admin123"}`
	resp, err := http.Post(base+"/auth/login", "application/json", strings.NewReader(body))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var data map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&data)
	if t, ok := data["access_token"].(string); ok {
		return t
	}
	return ""
}

func runLoadTest(name string, concurrency int, duration time.Duration, reqFn func() (*http.Response, error)) LoadTestResult {
	var total, success, failed int64
	var totalLatency int64
	var maxLat int64
	codes := make(map[int]int64)
	var codeMu sync.Mutex
	latencies := make([]int64, 0, 10000)
	var latMu sync.Mutex

	done := make(chan struct{})
	start := time.Now()

	go func() {
		time.Sleep(duration)
		close(done)
	}()

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}

				reqStart := time.Now()
				resp, err := reqFn()
				lat := time.Since(reqStart).Microseconds()

				atomic.AddInt64(&total, 1)
				atomic.AddInt64(&totalLatency, lat)

				latMu.Lock()
				latencies = append(latencies, lat)
				latMu.Unlock()

				for {
					old := atomic.LoadInt64(&maxLat)
					if lat <= old || atomic.CompareAndSwapInt64(&maxLat, old, lat) {
						break
					}
				}

				if err != nil {
					atomic.AddInt64(&failed, 1)
					continue
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				codeMu.Lock()
				codes[resp.StatusCode]++
				codeMu.Unlock()

				if resp.StatusCode >= 200 && resp.StatusCode < 400 {
					atomic.AddInt64(&success, 1)
				} else {
					atomic.AddInt64(&failed, 1)
				}
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	var avgLat, p99Lat time.Duration
	if total > 0 {
		avgLat = time.Duration(totalLatency/total) * time.Microsecond
	}
	// Simple P99
	latMu.Lock()
	if len(latencies) > 0 {
		// Sort a copy
		sorted := make([]int64, len(latencies))
		copy(sorted, latencies)
		for i := 0; i < len(sorted); i++ {
			for j := i + 1; j < len(sorted); j++ {
				if sorted[i] > sorted[j] {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				}
			}
		}
		idx := int(float64(len(sorted)) * 0.99)
		if idx >= len(sorted) {
			idx = len(sorted) - 1
		}
		p99Lat = time.Duration(sorted[idx]) * time.Microsecond
	}
	latMu.Unlock()

	return LoadTestResult{
		Name:          name,
		TotalRequests: total,
		Successful:    success,
		Failed:        failed,
		Duration:      elapsed,
		RPS:           float64(total) / elapsed.Seconds(),
		AvgLatency:    avgLat,
		P99Latency:    p99Lat,
		MaxLatency:    time.Duration(maxLat) * time.Microsecond,
		StatusCodes:   codes,
	}
}

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║  INEC Platform Load Test — 176K Polling Unit Simulation     ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	token := getToken()
	if token == "" {
		fmt.Println("ERROR: Could not get auth token")
		return
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
			MaxConnsPerHost:     1000,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	allResults := []LoadTestResult{}

	// Test 1: Health check baseline
	fmt.Println("▶ [1/7] Health Check Baseline (100 concurrent, 10s)...")
	r1 := runLoadTest("Health Check", 100, 10*time.Second, func() (*http.Response, error) {
		return client.Get(base + "/healthz")
	})
	allResults = append(allResults, r1)
	printResult(r1)

	// Test 2: Concurrent result submissions (election day simulation)
	fmt.Println("▶ [2/7] Result Submissions — Election Day (200 concurrent, 15s)...")
	r2 := runLoadTest("Result Submissions", 200, 15*time.Second, func() (*http.Response, error) {
		puCode := fmt.Sprintf("%02d-%02d-%02d-%03d", rand.Intn(37)+1, rand.Intn(20)+1, rand.Intn(10)+1, rand.Intn(500)+1)
		body := fmt.Sprintf(`{
			"election_id": 1,
			"polling_unit_code": "%s",
			"party_code": "APC",
			"votes": %d,
			"accredited_voters": %d,
			"registered_voters": %d
		}`, puCode, rand.Intn(500)+50, rand.Intn(800)+200, rand.Intn(1500)+500)
		req, _ := http.NewRequest("POST", base+"/results", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		return client.Do(req)
	})
	allResults = append(allResults, r2)
	printResult(r2)

	// Test 3: Dashboard queries under load (176K PU data fetch)
	fmt.Println("▶ [3/7] Dashboard Queries (150 concurrent, 10s)...")
	r3 := runLoadTest("Dashboard Queries", 150, 10*time.Second, func() (*http.Response, error) {
		req, _ := http.NewRequest("GET", base+"/dashboard", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		return client.Do(req)
	})
	allResults = append(allResults, r3)
	printResult(r3)

	// Test 4: Anomaly detection under load
	fmt.Println("▶ [4/7] Anomaly Detection (100 concurrent, 10s)...")
	r4 := runLoadTest("Anomaly Detection", 100, 10*time.Second, func() (*http.Response, error) {
		body := fmt.Sprintf(`{
			"polling_unit_code": "%02d-%02d-%02d-%03d",
			"registered_voters": %d,
			"accredited_voters": %d,
			"total_valid_votes": %d,
			"rejected_votes": %d
		}`, rand.Intn(37)+1, rand.Intn(20)+1, rand.Intn(10)+1, rand.Intn(500)+1,
			rand.Intn(2000)+500, rand.Intn(1500)+200, rand.Intn(1200)+100, rand.Intn(50))
		req, _ := http.NewRequest("POST", base+"/anomaly/check", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		return client.Do(req)
	})
	allResults = append(allResults, r4)
	printResult(r4)

	// Test 5: Mixed workload (realistic election day traffic)
	fmt.Println("▶ [5/7] Mixed Election Day Workload (300 concurrent, 20s)...")
	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{"GET", "/results?election_id=1", ""},
		{"GET", "/dashboard", ""},
		{"GET", "/command-center/state-velocities", ""},
		{"GET", "/polling-units?state=Lagos", ""},
		{"GET", "/collation/states", ""},
		{"GET", "/elections", ""},
		{"POST", "/results", `{"election_id":1,"polling_unit_code":"25-01-01-001","party_code":"PDP","votes":150}`},
		{"POST", "/incidents", `{"description":"Ballot box damaged","severity":"high","polling_unit_code":"25-01-01-001"}`},
		{"GET", "/observers?election_id=1", ""},
		{"GET", "/healthz", ""},
	}
	r5 := runLoadTest("Mixed Workload", 300, 20*time.Second, func() (*http.Response, error) {
		ep := endpoints[rand.Intn(len(endpoints))]
		var req *http.Request
		if ep.method == "GET" {
			req, _ = http.NewRequest("GET", base+ep.path, nil)
		} else {
			req, _ = http.NewRequest("POST", base+ep.path, strings.NewReader(ep.body))
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		return client.Do(req)
	})
	allResults = append(allResults, r5)
	printResult(r5)

	// Test 6: Connection pool stress test
	fmt.Println("▶ [6/7] Connection Pool Stress (500 concurrent, 10s)...")
	r6 := runLoadTest("Connection Pool Stress", 500, 10*time.Second, func() (*http.Response, error) {
		req, _ := http.NewRequest("GET", base+"/results?election_id=1", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		return client.Do(req)
	})
	allResults = append(allResults, r6)
	printResult(r6)

	// Test 7: WebSocket flood (command center SSE)
	fmt.Println("▶ [7/7] SSE Stream Flood (50 concurrent, 10s)...")
	r7 := runLoadTest("SSE Stream", 50, 10*time.Second, func() (*http.Response, error) {
		req, _ := http.NewRequest("GET", base+"/command-center/stream", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "text/event-stream")
		return client.Do(req)
	})
	allResults = append(allResults, r7)
	printResult(r7)

	// Summary
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════")
	fmt.Println("                     LOAD TEST SUMMARY")
	fmt.Println("════════════════════════════════════════════════════════════════")
	fmt.Printf("%-30s %10s %10s %10s %10s %10s\n", "Test", "Total", "RPS", "Avg(ms)", "P99(ms)", "Success%")
	fmt.Println(strings.Repeat("─", 80))
	for _, r := range allResults {
		successRate := float64(0)
		if r.TotalRequests > 0 {
			successRate = float64(r.Successful) / float64(r.TotalRequests) * 100
		}
		fmt.Printf("%-30s %10d %10.0f %10.1f %10.1f %9.1f%%\n",
			r.Name, r.TotalRequests, r.RPS,
			float64(r.AvgLatency.Microseconds())/1000,
			float64(r.P99Latency.Microseconds())/1000,
			successRate)
	}

	// JSON output
	report, _ := json.MarshalIndent(map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"results":   allResults,
	}, "", "  ")
	fmt.Println()
	fmt.Println(string(report))
}

func printResult(r LoadTestResult) {
	successRate := float64(0)
	if r.TotalRequests > 0 {
		successRate = float64(r.Successful) / float64(r.TotalRequests) * 100
	}
	fmt.Printf("  → %d requests | %.0f RPS | avg %.1fms | P99 %.1fms | max %.1fms | %.1f%% success\n",
		r.TotalRequests, r.RPS,
		float64(r.AvgLatency.Microseconds())/1000,
		float64(r.P99Latency.Microseconds())/1000,
		float64(r.MaxLatency.Microseconds())/1000,
		successRate)
	fmt.Println()
}
