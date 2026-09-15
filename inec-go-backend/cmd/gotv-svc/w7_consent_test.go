package main

// R5-096 (PG-backed): supporter consent must be a provable record.
// Covers: structured consent creates a ledger row at contact creation,
// dispatch eligibility requires an ACTIVE consent record, opt-out withdraws
// consent (and thus dispatch eligibility), and the migration-000046 backfill
// labels legacy free-text consent ids honestly.

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gorilla/mux"
)

// dispatchEligible mirrors the consent predicate now used by both dispatch
// engines (internal/gotv/dispatch.go, dispatch_v2.go).
func dispatchEligible(t *testing.T, contactID string) bool {
	t.Helper()
	var n int
	if err := dbConn.QueryRow(
		`SELECT COUNT(*) FROM gotv_contacts c
		 WHERE c.contact_id=$1 AND c.opted_out = FALSE
		   AND EXISTS (SELECT 1 FROM gotv_consent_records cr WHERE cr.consent_id = c.consent_id AND cr.status='active')`,
		contactID).Scan(&n); err != nil {
		t.Fatalf("eligibility query: %v", err)
	}
	return n == 1
}

func TestConsentLedgerAndOptOut(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}
	// Apply migration 000046 (FK + legacy backfill) to the test schema. The
	// shared test DB persists between runs, so reset via the down migration
	// first (both are idempotent).
	if down, err := os.ReadFile("../../migrations/000046_gotv_consent_records.down.sql"); err == nil {
		if _, err := dbConn.Exec(string(down)); err != nil {
			t.Fatalf("apply down migration 000046: %v", err)
		}
	}
	mig, err := os.ReadFile("../../migrations/000046_gotv_consent_records.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := dbConn.Exec(string(mig)); err != nil {
		t.Fatalf("apply migration 000046: %v", err)
	}

	// 1. Contact created with structured consent → ledger row + eligible.
	body := map[string]interface{}{
		"phone": "08099990001", "full_name": "Consent Test",
		"consent": map[string]string{
			"channel": "field", "purpose": "campaign_outreach",
			"legal_basis": "consent", "proof_ref": "form-EC-CONSENT-1",
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/gotv/contacts", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GOTV-Party-ID", "1")
	req.Header.Set("X-GOTV-User", "w7-tester")
	rr := httptest.NewRecorder()
	handleCreateContact(rr, req)
	if rr.Code != 201 {
		t.Fatalf("create with consent returned %d: %s", rr.Code, rr.Body.String())
	}
	var created map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &created)
	contactID, _ := created["contact_id"].(string)

	var legalBasis, channel, status string
	if err := dbConn.QueryRow(
		`SELECT cr.legal_basis, cr.channel, cr.status FROM gotv_consent_records cr
		 JOIN gotv_contacts c ON c.consent_id = cr.consent_id WHERE c.contact_id=$1`, contactID).
		Scan(&legalBasis, &channel, &status); err != nil {
		t.Fatalf("consent record for new contact: %v", err)
	}
	if legalBasis != "consent" || channel != "field" || status != "active" {
		t.Fatalf("unexpected consent record: basis=%s channel=%s status=%s", legalBasis, channel, status)
	}
	if !dispatchEligible(t, contactID) {
		t.Fatal("contact with ACTIVE consent must be dispatch-eligible")
	}

	// 2. Contact without any consent → not dispatch-eligible.
	if _, err := dbConn.Exec(
		`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash)
		 VALUES ('w7-noconsent', 1, 'x', 'h-noconsent') ON CONFLICT (contact_id) DO NOTHING`); err != nil {
		t.Fatalf("seed noconsent contact: %v", err)
	}
	if dispatchEligible(t, "w7-noconsent") {
		t.Fatal("contact without consent must NOT be dispatch-eligible")
	}

	// 3. Opt-out → consent withdrawn → not eligible.
	req = httptest.NewRequest("POST", "/gotv/contacts/"+contactID+"/opt-out", nil)
	req.Header.Set("X-GOTV-Party-ID", "1")
	req.Header.Set("X-GOTV-User", "w7-tester")
	req = mux.SetURLVars(req, map[string]string{"id": contactID})
	rr = httptest.NewRecorder()
	handleOptOut(rr, req)
	if rr.Code != 200 {
		t.Fatalf("opt-out returned %d: %s", rr.Code, rr.Body.String())
	}
	var wStatus string
	var withdrawn *string
	if err := dbConn.QueryRow(
		`SELECT cr.status, cr.withdrawn_at::text FROM gotv_consent_records cr
		 JOIN gotv_contacts c ON c.consent_id = cr.consent_id WHERE c.contact_id=$1`, contactID).
		Scan(&wStatus, &withdrawn); err != nil {
		t.Fatalf("read consent after opt-out: %v", err)
	}
	if wStatus != "withdrawn" || withdrawn == nil {
		t.Fatalf("opt-out must withdraw consent, got status=%s withdrawn_at=%v", wStatus, withdrawn)
	}
	if dispatchEligible(t, contactID) {
		t.Fatal("opted-out contact must NOT be dispatch-eligible")
	}

	// 4. FK enforcement: a contact can no longer be linked to a consent_id
	// that has no consent record (the pre-migration free-text hole).
	if _, err := dbConn.Exec(
		`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash, consent_id)
		 VALUES ('w7-legacy', 1, 'x', 'h-legacy', 'unrecorded-consent-1')`); err == nil {
		t.Fatal("FK must reject a contact linked to an unrecorded consent_id")
	}

	// 5. Backfill invariant: every consent_id present before the migration
	// (e.g. seed data with free-text ids) has an honestly labelled record.
	var orphan int
	if err := dbConn.QueryRow(
		`SELECT COUNT(*) FROM gotv_contacts c
		 WHERE c.consent_id IS NOT NULL
		   AND NOT EXISTS (SELECT 1 FROM gotv_consent_records cr WHERE cr.consent_id = c.consent_id)`).Scan(&orphan); err != nil {
		t.Fatalf("orphan check: %v", err)
	}
	if orphan != 0 {
		t.Fatalf("%d contacts have consent_id without a consent record (backfill incomplete)", orphan)
	}
	var legacyBasis *string
	_ = dbConn.QueryRow(
		`SELECT legal_basis FROM gotv_consent_records WHERE recorded_by='migration_000046' LIMIT 1`).Scan(&legacyBasis)
	if legacyBasis != nil && *legacyBasis != "legacy_asserted" {
		t.Fatalf("backfilled consent must be labelled legacy_asserted, got %s", *legacyBasis)
	}
}
