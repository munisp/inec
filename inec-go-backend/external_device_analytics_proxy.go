package main

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func lakehouseAnalyticsBaseURL() (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("LAKEHOUSE_URL")), "/")
	if baseURL == "" {
		return "", errIReVUnavailable
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errIReVUnavailable
	}
	return baseURL, nil
}

func handleExternalDeviceQuality(w http.ResponseWriter, r *http.Request) {
	if _, err := requireRole(r, "admin", "ict_officer", "security", "collation_officer", "observer"); err != nil {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	baseURL, err := lakehouseAnalyticsBaseURL()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "lakehouse analytics is not configured")
		return
	}
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "100"
	}
	parsedLimit, err := strconv.Atoi(limit)
	if err != nil || parsedLimit < 1 || parsedLimit > 1000 {
		writeError(w, http.StatusBadRequest, "limit must be between 1 and 1000")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, baseURL+"/analytics/external-device-quality?limit="+strconv.Itoa(parsedLimit), nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not create lakehouse request")
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "lakehouse external-device quality is unavailable")
		return
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not read lakehouse response")
		return
	}
	if response.StatusCode != http.StatusOK {
		writeError(w, http.StatusServiceUnavailable, "lakehouse external-device quality is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
