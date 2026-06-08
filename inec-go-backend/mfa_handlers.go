package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"inec-go-backend/internal/auth"
	"github.com/rs/zerolog/log"
)

var mfaService *auth.MFAService

func initMFA() {
	mfaService = auth.NewMFAService(db, "INEC Platform")
	if err := mfaService.InitTables(context.Background()); err != nil {
		log.Error().Err(err).Msg("Failed to create MFA tables")
	}
	log.Info().Msg("MFA service initialized (TOTP + WebAuthn + backup codes)")
}

// handleMFASetup initiates MFA enrollment — generates TOTP secret + backup codes.
func handleMFASetup(w http.ResponseWriter, r *http.Request) {
	claims, err := getCurrentUser(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	userID, _ := strconv.Atoi(claims["sub"].(string))
	username := claims["username"].(string)

	setup, err := mfaService.SetupTOTP(r.Context(), userID, username)
	if err != nil {
		log.Error().Err(err).Int("user_id", userID).Msg("MFA setup failed")
		http.Error(w, `{"error":"mfa setup failed"}`, 500)
		return
	}

	writeJSON(w, 200, setup)
}

// handleMFAVerifySetup validates the 6-digit code during initial setup to confirm device.
func handleMFAVerifySetup(w http.ResponseWriter, r *http.Request) {
	claims, err := getCurrentUser(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		http.Error(w, `{"error":"code is required"}`, 400)
		return
	}

	userID, _ := strconv.Atoi(claims["sub"].(string))
	valid, err := mfaService.VerifyTOTP(r.Context(), userID, req.Code)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, 400)
		return
	}
	if !valid {
		http.Error(w, `{"error":"invalid code"}`, 401)
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"verified": true,
		"message":  "MFA enabled successfully",
	})
}

// handleMFADisable disables MFA after verifying current code.
func handleMFADisable(w http.ResponseWriter, r *http.Request) {
	claims, err := getCurrentUser(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		http.Error(w, `{"error":"current TOTP code required to disable"}`, 400)
		return
	}

	userID, _ := strconv.Atoi(claims["sub"].(string))
	valid, err := mfaService.VerifyTOTP(r.Context(), userID, req.Code)
	if err != nil || !valid {
		http.Error(w, `{"error":"invalid code"}`, 401)
		return
	}

	if err := mfaService.DisableTOTP(r.Context(), userID); err != nil {
		http.Error(w, `{"error":"failed to disable MFA"}`, 500)
		return
	}

	writeJSON(w, 200, map[string]interface{}{"message": "MFA disabled"})
}

// handleMFAWebAuthnBegin starts WebAuthn registration ceremony.
func handleMFAWebAuthnBegin(w http.ResponseWriter, r *http.Request) {
	claims, err := getCurrentUser(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	userID, _ := strconv.Atoi(claims["sub"].(string))
	username := claims["username"].(string)

	options, err := mfaService.BeginWebAuthnRegistration(r.Context(), userID, username)
	if err != nil {
		http.Error(w, `{"error":"failed to begin registration"}`, 500)
		return
	}

	writeJSON(w, 200, options)
}

// handleMFAWebAuthnComplete finishes WebAuthn registration.
func handleMFAWebAuthnComplete(w http.ResponseWriter, r *http.Request) {
	claims, err := getCurrentUser(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	var req struct {
		CredentialID []byte `json:"credential_id"`
		PublicKey    []byte `json:"public_key"`
		DeviceName   string `json:"device_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, 400)
		return
	}
	if req.DeviceName == "" {
		req.DeviceName = "Unknown Device"
	}

	userID, _ := strconv.Atoi(claims["sub"].(string))
	if err := mfaService.CompleteWebAuthnRegistration(r.Context(), userID, req.CredentialID, req.PublicKey, req.DeviceName); err != nil {
		http.Error(w, `{"error":"registration failed: `+err.Error()+`"}`, 500)
		return
	}

	writeJSON(w, 200, map[string]interface{}{"registered": true, "device": req.DeviceName})
}

// handleMFAWebAuthnList returns registered WebAuthn credentials.
func handleMFAWebAuthnList(w http.ResponseWriter, r *http.Request) {
	claims, err := getCurrentUser(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	userID, _ := strconv.Atoi(claims["sub"].(string))
	creds, err := mfaService.ListWebAuthnCredentials(r.Context(), userID)
	if err != nil {
		http.Error(w, `{"error":"failed to list credentials"}`, 500)
		return
	}
	if creds == nil {
		creds = []auth.WebAuthnCredential{}
	}

	writeJSON(w, 200, map[string]interface{}{"credentials": creds, "total": len(creds)})
}

// handleMFABackupCodes regenerates backup codes (requires current code verification).
func handleMFABackupCodes(w http.ResponseWriter, r *http.Request) {
	claims, err := getCurrentUser(r)
	if err != nil {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		http.Error(w, `{"error":"current TOTP code required"}`, 400)
		return
	}

	userID, _ := strconv.Atoi(claims["sub"].(string))
	valid, err := mfaService.VerifyTOTP(r.Context(), userID, req.Code)
	if err != nil || !valid {
		http.Error(w, `{"error":"invalid code"}`, 401)
		return
	}

	codes, err := mfaService.RegenerateBackupCodes(r.Context(), userID)
	if err != nil {
		http.Error(w, `{"error":"failed to generate backup codes"}`, 500)
		return
	}

	writeJSON(w, 200, map[string]interface{}{
		"backup_codes": codes,
		"count":        len(codes),
		"warning":      "These codes can only be used once. Store them securely.",
	})
}
