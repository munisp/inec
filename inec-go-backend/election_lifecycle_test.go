package main

// Focused PG-backed tests for the W2 election-lifecycle core:
// declaration gate, voting-window submission, correction supersession,
// rerun merge math. Requires a PostgreSQL DSN via TEST_DATABASE_URL /
// DATABASE_URL (or the sandbox pgserver socket).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
)

var lifecycleTestReady bool

func setupLifecycleTestDB(t *testing.T) {
	t.Helper()
	if lifecycleTestReady && db != nil {
		return
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "host=/home/kimi/pgdata user=postgres dbname=postgres sslmode=disable"
	}
	admin := openDatabase(dsn)
	defer admin.Close()
	// Fresh database per run for determinism.
	dbName := "w2_lifecycle_test"
	admin.Exec("DROP DATABASE IF EXISTS " + dbName)
	if _, err := admin.Exec("CREATE DATABASE " + dbName); err != nil {
		t.Skipf("cannot create test database (PG unavailable?): %v", err)
	}
	testDSN := strings.Replace(dsn, "dbname=postgres", "dbname="+dbName, 1)
	if !strings.Contains(testDSN, "dbname=") {
		testDSN = dsn + " dbname=" + dbName
	}
	db = openDatabase(testDSN)
	initScaledDB(db)
	if err := runMigrations(db); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}
	initValidator()
	initEvidenceIntegritySchema()
	initElectionFSMSchema()
	initDisputeSchema()
	mwHub = initMiddlewareHub()

	// Seed minimal geo hierarchy: 3 states (incl. FCT), 1 LGA/ward each, 4 PUs.
	exec := func(q string, args ...interface{}) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("seed failed: %v\n%s", err, q)
		}
	}
	exec("INSERT INTO states (code, name, geo_zone) VALUES ('FCT','Federal Capital Territory','north_central'),('AB','Abia','south_east'),('KD','Kaduna','north_west')")
	exec("INSERT INTO lgas (code, name, state_code) VALUES ('FCT-AMAC','AMAC','FCT'),('AB-UMU','Umuahia North','AB'),('KD-KAD','Kaduna North','KD')")
	exec("INSERT INTO wards (code, name, lga_code) VALUES ('FCT-W1','Ward 1','FCT-AMAC'),('AB-W1','Ward 1','AB-UMU'),('KD-W1','Ward 1','KD-KAD')")
	exec(`INSERT INTO polling_units (code, name, ward_code, registered_voters, latitude, longitude) VALUES
		('PU-001','PU One','AB-W1',500,5.53,7.49),
		('PU-002','PU Two','AB-W1',500,5.53,7.49),
		('PU-003','PU Three','FCT-W1',500,9.06,7.49),
		('PU-004','PU Four','KD-W1',500,10.52,7.44)`)
	exec("INSERT INTO parties (code, name, abbreviation) VALUES ('APC','All Progressives Congress','APC'),('PDP','Peoples Democratic Party','PDP'),('LP','Labour Party','LP')")
	exec("INSERT INTO users (username, password_hash, full_name, role, state_code) VALUES ('officer1','x','Officer One','presiding_officer','AB'),('ro1','x','Returning Officer','collation_officer',NULL),('admin1','x','Admin','admin',NULL)")
	lifecycleTestReady = true
}

func doRequest(handler http.HandlerFunc, method, path, body string, claims jwt.MapClaims, vars map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, claims))
	}
	if vars != nil {
		req = mux.SetURLVars(req, vars)
	}
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func mkElection(t *testing.T, status, kind string, parentID interface{}) int {
	t.Helper()
	var id int
	err := db.QueryRow(`INSERT INTO elections (title, election_type, election_date, status, election_kind, parent_election_id)
		VALUES ('Test Election','gubernatorial','2026-03-01',$1,$2,$3) RETURNING id`, status, kind, parentID).Scan(&id)
	if err != nil {
		t.Fatalf("create election: %v", err)
	}
	return id
}

func mkResult(t *testing.T, electionID int, pu, status string, apc, pdp int) int64 {
	t.Helper()
	var id int64
	valid := apc + pdp
	err := db.QueryRow(`INSERT INTO results (election_id, polling_unit_code, status, total_valid_votes, rejected_votes, total_votes_cast, accredited_voters, finalized_at)
		VALUES ($1,$2,$3,$4,5,$5,$6, CASE WHEN $3='finalized' THEN CURRENT_TIMESTAMP ELSE NULL END) RETURNING id`,
		electionID, pu, status, valid, valid+5, valid+10).Scan(&id)
	if err != nil {
		t.Fatalf("create result: %v", err)
	}
	for party, v := range map[string]int{"APC": apc, "PDP": pdp} {
		if _, err := db.Exec("INSERT INTO result_party_scores (result_id, party_code, votes) VALUES ($1,$2,$3)", id, party, v); err != nil {
			t.Fatalf("create party score: %v", err)
		}
	}
	return id
}

var officerClaims = jwt.MapClaims{"sub": "1", "username": "officer1", "role": "presiding_officer", "state_code": "AB"}
var roClaims = jwt.MapClaims{"sub": "2", "username": "ro1", "role": "returning_officer"}
var adminClaims = jwt.MapClaims{"sub": "3", "username": "admin1", "role": "admin"}

// R5-012: submissions are accepted in BOTH 'active' and 'voting' states and
// rejected once the election leaves the capture window.
func TestSubmitAcceptedDuringVoting(t *testing.T) {
	setupLifecycleTestDB(t)
	for _, status := range []string{"active", "voting"} {
		eid := mkElection(t, status, "general", nil)
		body := fmt.Sprintf(`{"election_id":%d,"polling_unit_code":"PU-001","party_scores":[{"party_code":"APC","votes":200},{"party_code":"PDP","votes":90}],"accredited_voters":300,"rejected_votes":5,"device_lat":5.53,"device_lng":7.49}`, eid)
		w := doRequest(handleSubmitResult, "POST", "/results/submit", body, officerClaims, nil)
		if w.Code != 200 {
			t.Fatalf("submit in status %s: got %d: %s", status, w.Code, w.Body.String())
		}
		db.Exec("DELETE FROM results WHERE election_id=$1", eid)
	}
	eid := mkElection(t, "closed", "general", nil)
	body := fmt.Sprintf(`{"election_id":%d,"polling_unit_code":"PU-001","party_scores":[{"party_code":"APC","votes":10},{"party_code":"PDP","votes":5}],"accredited_voters":20,"rejected_votes":0,"device_lat":5.53,"device_lng":7.49}`, eid)
	w := doRequest(handleSubmitResult, "POST", "/results/submit", body, officerClaims, nil)
	if w.Code == 200 {
		t.Fatalf("submit in closed election unexpectedly accepted")
	}
}

// R5-011: declaration is blocked while PUs are unaccounted for and succeeds
// with winner persistence once complete.
func TestDeclarationGateAndWinner(t *testing.T) {
	setupLifecycleTestDB(t)
	eid := mkElection(t, "collating", "general", nil)
	mkResult(t, eid, "PU-001", "finalized", 200, 100)
	mkResult(t, eid, "PU-002", "finalized", 150, 120)

	// Gate blocks: PU-003, PU-004 unaccounted.
	w := doRequest(handleDeclareResult, "POST", fmt.Sprintf("/elections/%d/declare", eid), `{}`, roClaims, map[string]string{"id": fmt.Sprint(eid)})
	if w.Code != 422 {
		t.Fatalf("incomplete declaration: expected 422, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "not all polling units are accounted for") {
		t.Fatalf("expected completeness gate error, got: %s", w.Body.String())
	}

	// Complete the election and declare.
	mkResult(t, eid, "PU-003", "finalized", 80, 90)
	mkResult(t, eid, "PU-004", "finalized", 100, 110)
	w = doRequest(handleDeclareResult, "POST", fmt.Sprintf("/elections/%d/declare", eid), `{"notes":"test declaration"}`, roClaims, map[string]string{"id": fmt.Sprint(eid)})
	if w.Code != 200 {
		t.Fatalf("complete declaration: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var status, declaredBy string
	var declaredAt interface{}
	var winnerPayload string
	err := db.QueryRow("SELECT status, declared_by, declared_at, winner_payload FROM elections WHERE id=$1", eid).
		Scan(&status, &declaredBy, &declaredAt, &winnerPayload)
	if err != nil {
		t.Fatalf("read declaration: %v", err)
	}
	if status != "declared" || declaredBy != "ro1" || declaredAt == nil {
		t.Fatalf("declaration not persisted: status=%s by=%s at=%v", status, declaredBy, declaredAt)
	}
	var payload map[string]interface{}
	json.Unmarshal([]byte(winnerPayload), &payload)
	if payload["winner_party"] != "APC" {
		t.Fatalf("wrong winner: %v", payload["winner_party"])
	}
	// Audit trail has the declaration.
	var auditCount int
	db.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action='RESULT_DECLARED' AND entity_type='election' AND entity_id=$1", fmt.Sprint(eid)).Scan(&auditCount)
	if auditCount == 0 {
		t.Fatalf("declaration missing from audit log")
	}
	// Double declaration is rejected.
	w = doRequest(handleDeclareResult, "POST", fmt.Sprintf("/elections/%d/declare", eid), `{}`, roClaims, map[string]string{"id": fmt.Sprint(eid)})
	if w.Code != 409 {
		t.Fatalf("re-declaration: expected 409, got %d", w.Code)
	}
}

// R5-016: a correction supersedes the old result (history preserved) and the
// corrected result becomes the only canonical one.
func TestCorrectionSupersedes(t *testing.T) {
	setupLifecycleTestDB(t)
	eid := mkElection(t, "collating", "general", nil)
	oldID := mkResult(t, eid, "PU-001", "finalized", 200, 100) // transposed: APC/PDP swapped below

	body := `{"party_scores":[{"party_code":"APC","votes":100},{"party_code":"PDP","votes":200}],"accredited_voters":310,"rejected_votes":5,"reason":"PO transposed party scores"}`
	w := doRequest(handleCorrectResult, "POST", fmt.Sprintf("/results/%d/correct", oldID), body, adminClaims, map[string]string{"id": fmt.Sprint(oldID)})
	if w.Code != 201 {
		t.Fatalf("correction: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var oldStatus string
	db.QueryRow("SELECT status FROM results WHERE id=$1", oldID).Scan(&oldStatus)
	if oldStatus != "superseded" {
		t.Fatalf("old result not superseded: %s", oldStatus)
	}
	var newID int64
	var newStatus string
	var supersedes interface{}
	db.QueryRow("SELECT id, status, supersedes_result_id FROM results WHERE election_id=$1 AND polling_unit_code='PU-001' AND status <> 'superseded'", eid).Scan(&newID, &newStatus, &supersedes)
	if newStatus != "pending" || supersedes == nil {
		t.Fatalf("corrected result wrong: id=%d status=%s supersedes=%v", newID, newStatus, supersedes)
	}
	var corrCount int
	db.QueryRow("SELECT COUNT(*) FROM result_corrections WHERE superseded_result_id=$1 AND new_result_id=$2", oldID, newID).Scan(&corrCount)
	if corrCount != 1 {
		t.Fatalf("correction trail missing")
	}
	// Canonical reads exclude the superseded row.
	w = doRequest(handleListResults, "GET", fmt.Sprintf("/results?election_id=%d", eid), "", nil, nil)
	if strings.Contains(w.Body.String(), fmt.Sprintf(`"id":%d`, oldID)) && !strings.Contains(w.Body.String(), `"total":0`) {
		t.Fatalf("superseded result leaked into canonical list: %s", w.Body.String())
	}
	// Audit logged.
	var auditCount int
	db.QueryRow("SELECT COUNT(*) FROM audit_log WHERE action='RESULT_CORRECTED' AND entity_id=$1", fmt.Sprint(oldID)).Scan(&auditCount)
	if auditCount == 0 {
		t.Fatalf("correction missing from audit log")
	}
}

// R5-018: rerun election results merge into the parent election's canonical
// collation (supplementary semantics: rerun figures add to parent totals).
func TestRerunMergeMath(t *testing.T) {
	setupLifecycleTestDB(t)
	parent := mkElection(t, "collating", "general", nil)
	mkResult(t, parent, "PU-001", "finalized", 100, 50)
	mkResult(t, parent, "PU-002", "finalized", 100, 50)
	mkResult(t, parent, "PU-003", "finalized", 100, 50)
	mkResult(t, parent, "PU-004", "voided", 0, 0) // cancelled in main poll

	// Create a supplementary election scoped to PU-004.
	body := `{"kind":"supplementary","election_date":"2026-03-08","scope":[{"scope_type":"polling_unit","area_code":"PU-004","reason":"cancelled due to overvoting"}]}`
	w := doRequest(handleCreateRerun, "POST", fmt.Sprintf("/elections/%d/reruns", parent), body, adminClaims, map[string]string{"id": fmt.Sprint(parent)})
	if w.Code != 201 {
		t.Fatalf("create rerun: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var rerunID int
	db.QueryRow("SELECT id FROM elections WHERE parent_election_id=$1", parent).Scan(&rerunID)
	db.Exec("UPDATE elections SET status='voting' WHERE id=$1", rerunID)

	// Out-of-scope submission must be rejected.
	kdOfficer := jwt.MapClaims{"sub": "1", "username": "officer1", "role": "presiding_officer", "state_code": "AB"}
	subBody := fmt.Sprintf(`{"election_id":%d,"polling_unit_code":"PU-001","party_scores":[{"party_code":"APC","votes":10},{"party_code":"PDP","votes":5}],"accredited_voters":20,"rejected_votes":0,"device_lat":5.53,"device_lng":7.49}`, rerunID)
	w = doRequest(handleSubmitResult, "POST", "/results/submit", subBody, kdOfficer, nil)
	if w.Code != 403 {
		t.Fatalf("out-of-scope rerun submission: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// In-scope submission accepted (KD officer).
	kdClaims := jwt.MapClaims{"sub": "1", "username": "officer1", "role": "presiding_officer", "state_code": "KD"}
	subBody = fmt.Sprintf(`{"election_id":%d,"polling_unit_code":"PU-004","party_scores":[{"party_code":"APC","votes":40},{"party_code":"PDP","votes":35}],"accredited_voters":80,"rejected_votes":2,"device_lat":10.52,"device_lng":7.44}`, rerunID)
	w = doRequest(handleSubmitResult, "POST", "/results/submit", subBody, kdClaims, nil)
	if w.Code != 200 {
		t.Fatalf("in-scope rerun submission: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Finalize the rerun result through the normal pipeline.
	var resID string
	db.QueryRow("SELECT id FROM results WHERE election_id=$1 AND polling_unit_code='PU-004'", rerunID).Scan(&resID)
	w = doRequest(handleValidateResult, "POST", "/results/"+resID+"/validate", "{}", adminClaims, map[string]string{"id": resID})
	if w.Code != 200 {
		t.Fatalf("validate rerun result: %d: %s", w.Code, w.Body.String())
	}
	w = doRequest(handleFinalizeResult, "POST", "/results/"+resID+"/finalize", "{}", adminClaims, map[string]string{"id": resID})
	if w.Code != 200 {
		t.Fatalf("finalize rerun result: %d: %s", w.Code, w.Body.String())
	}

	// Canonical parent collation: APC = 100*3 + 40 = 340, PDP = 50*3 + 35 = 185.
	totals, totalVotes, puCount, err := canonicalPartyTotals(context.Background(), parent, "national", "NG")
	if err != nil {
		t.Fatalf("canonical totals: %v", err)
	}
	if totals["APC"] != 340 || totals["PDP"] != 185 || puCount != 4 {
		t.Fatalf("rerun merge math wrong: totals=%v total=%d pus=%d", totals, totalVotes, puCount)
	}
}
