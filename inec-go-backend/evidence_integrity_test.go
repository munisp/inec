package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCanonicalJSONAndEventHashAreDeterministic(t *testing.T) {
	firstPayload := M{"polling_unit_code": "PU-001", "status": "pending", "total_votes_cast": 321}
	secondPayload := M{"total_votes_cast": 321, "status": "pending", "polling_unit_code": "PU-001"}

	firstHash, firstBytes, err := payloadHash(firstPayload)
	if err != nil {
		t.Fatalf("payloadHash first payload: %v", err)
	}
	secondHash, secondBytes, err := payloadHash(secondPayload)
	if err != nil {
		t.Fatalf("payloadHash second payload: %v", err)
	}
	if firstHash != secondHash || string(firstBytes) != string(secondBytes) {
		t.Fatalf("canonical JSON must be deterministic: %s != %s", firstBytes, secondBytes)
	}

	firstEventHash := integrityEventHash(17, 1, "RESULT_SUBMITTED", "", firstHash, 2, 0)
	if firstEventHash != integrityEventHash(17, 1, "RESULT_SUBMITTED", "", secondHash, 2, 0) {
		t.Fatal("identical canonical event inputs must produce an identical event hash")
	}
	if firstEventHash == integrityEventHash(17, 2, "RESULT_SUBMITTED", firstEventHash, firstHash, 2, 0) {
		t.Fatal("a changed sequence or predecessor must change the event hash")
	}
	if len(firstEventHash) != 64 {
		t.Fatalf("expected SHA-256 hex hash length 64, got %d", len(firstEventHash))
	}
}

func TestIntegritySigningRequirementFailsClosedInProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("INTEGRITY_SIGNING_REQUIRED", "")
	if !integritySigningRequired() {
		t.Fatal("production must require integrity signing")
	}
	if !integrityPolicyRequired() {
		t.Fatal("production must require an approved policy version")
	}

	t.Setenv("APP_ENV", "development")
	t.Setenv("INTEGRITY_SIGNING_REQUIRED", "true")
	if !integritySigningRequired() {
		t.Fatal("explicit integrity signing requirement must be honored")
	}
	if !integrityPolicyRequired() {
		t.Fatal("explicit integrity signing requirement must require a policy version")
	}
}

func TestOfficialVoterServiceURLRequiresHTTPS(t *testing.T) {
	t.Setenv("INEC_CVR_URL", "https://cvr.inecnigeria.org/")
	url, err := officialVoterServiceURL()
	if err != nil || url != "https://cvr.inecnigeria.org/" {
		t.Fatalf("expected configured official HTTPS URL, got %q, %v", url, err)
	}

	t.Setenv("INEC_CVR_URL", "http://untrusted.example")
	if _, err := officialVoterServiceURL(); err == nil {
		t.Fatal("non-HTTPS voter-service URLs must be rejected")
	}

	t.Setenv("INEC_CVR_URL", "://malformed")
	if _, err := officialVoterServiceURL(); err == nil {
		t.Fatal("malformed voter-service URLs must be rejected")
	}
}

func TestSignerHealthUsesPrivateServiceContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/integrity/health" {
			t.Fatalf("unexpected signer health request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-integrity-token" {
			t.Fatalf("unexpected signer authorization header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"healthy","signing_ready":true,"verifying_ready":true,"key_id":"test-key"}`))
	}))
	defer server.Close()

	previousURL := rustInferenceURL
	rustInferenceURL = server.URL
	t.Cleanup(func() { rustInferenceURL = previousURL })
	t.Setenv("APP_ENV", "production")
	t.Setenv("INTEGRITY_SERVICE_TOKEN", "test-integrity-token")

	result := checkIntegritySignerHealth(t.Context())
	if result["status"] != "healthy" || result["signing_ready"] != true || result["key_id"] != "test-key" {
		t.Fatalf("unexpected signer health result: %#v", result)
	}
}

func TestIntegrityValidationVocabularyIsConstrained(t *testing.T) {
	for _, caseType := range []string{"artifact_quality", "ocr_arithmetic", "material_manifest_mismatch", "other"} {
		if !validIntegrityCaseType(caseType) {
			t.Fatalf("expected supported reconciliation case type %q", caseType)
		}
	}
	for _, invalid := range []string{"unknown", "", "MODEL_APPROVED"} {
		if validIntegrityCaseType(invalid) {
			t.Fatalf("unexpected reconciliation case type accepted: %q", invalid)
		}
	}
	for _, severity := range []string{"low", "medium", "high", "critical"} {
		if !validIntegritySeverity(severity) {
			t.Fatalf("expected supported severity %q", severity)
		}
	}
	if validIntegritySeverity("informational") {
		t.Fatal("unsupported severity must be rejected")
	}

	if !validMaterialType("ec8a_template") || validMaterialType("runtime_seed") {
		t.Fatal("material manifest types must remain allow-listed")
	}
}

func TestEC8AValidationRejectsArithmeticMismatch(t *testing.T) {
	form := &FormEC8A{
		ElectionID:       1,
		PollingUnitCode:  "PU-TEST-001",
		RegisteredVoters: 100,
		AccreditedVoters: 80,
		TotalVotesPolled: 80,
		RejectedBallots:  5,
		TotalValidVotes:  74,
		PartyResults: []PartyVoteEntry{
			{PartyCode: "AAA", Votes: 40},
			{PartyCode: "BBB", Votes: 34},
		},
	}
	violations := ValidateEC8A(form)
	if len(violations) == 0 {
		t.Fatal("an arithmetic mismatch must produce a validation violation")
	}
	joined := strings.Join(violations, " ")
	if !strings.Contains(joined, "valid_votes") {
		t.Fatalf("expected valid-vote mismatch violation, got %v", violations)
	}
}
