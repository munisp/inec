package main

import (
	"net/http"
	"strings"
)

// inputValidationMiddleware rejects malformed or dangerous requests early in the chain.
func inputValidationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject requests with null bytes in URL (path traversal vector)
		if strings.ContainsRune(r.URL.Path, 0) {
			writeError(w, 400, "invalid URL: null bytes not allowed")
			return
		}

		// Enforce Content-Type for mutation methods with bodies
		if r.ContentLength > 0 && (r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH") {
			ct := r.Header.Get("Content-Type")
			if ct == "" {
				writeError(w, 415, "Content-Type header required for request body")
				return
			}
			// Allow JSON, form-data, and multipart only
			if !strings.HasPrefix(ct, "application/json") &&
				!strings.HasPrefix(ct, "application/x-www-form-urlencoded") &&
				!strings.HasPrefix(ct, "multipart/form-data") {
				writeError(w, 415, "unsupported Content-Type")
				return
			}
		}

		// Enforce max request body size (50 MB)
		if r.ContentLength > 50*1024*1024 {
			writeError(w, 413, "request body too large (max 50MB)")
			return
		}

		next.ServeHTTP(w, r)
	})
}
