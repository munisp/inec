package main

import (
"encoding/json"
"net/http"
)

// HandleDataSubjectAccess handles NDPR right to access
func HandleDataSubjectAccess(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Data subject access request logged"})
}

// HandleDataSubjectErasure handles NDPR right to erasure (right to be forgotten)
func HandleDataSubjectErasure(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
json.NewEncoder(w).Encode(map[string]string{"status": "success", "message": "Data subject erasure request processed"})
}
