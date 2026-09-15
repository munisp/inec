package main

// R5-100: a driver no-show/cancellation must clear the dead assignment and
// return the ride to pending for re-match instead of stranding the voter.

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func seedRideFixture(t *testing.T, status string) string {
	t.Helper()
	partyID := 1
	if _, err := dbConn.Exec(`INSERT INTO gotv_contacts (contact_id, party_id, phone_encrypted, phone_hash)
		VALUES ('w7-contact', $1, 'x', 'h') ON CONFLICT (contact_id) DO NOTHING`, partyID); err != nil {
		t.Fatalf("seed contact: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO gotv_volunteers (volunteer_id, party_id, full_name, phone, role, has_vehicle, latitude, longitude)
		VALUES ('w7-driver', $1, 'W7 Driver', '0800', 'driver', TRUE, 6.5, 3.4) ON CONFLICT (volunteer_id) DO NOTHING`, partyID); err != nil {
		t.Fatalf("seed volunteer: %v", err)
	}
	rideID := "w7-ride-" + status
	dbConn.Exec(`DELETE FROM gotv_ride_requests WHERE request_id=$1`, rideID)
	vol := interface{}(nil)
	matchedAt := interface{}(nil)
	if status == "matched" {
		vol = "w7-driver"
		matchedAt = "now"
	}
	if _, err := dbConn.Exec(`INSERT INTO gotv_ride_requests
		(request_id, party_id, contact_id, volunteer_id, pickup_latitude, pickup_longitude, polling_unit_code, status, matched_at)
		VALUES ($1, $2, 'w7-contact', $3, 6.5244, 3.3792, 'LA-PU-0001', $4, CASE WHEN $5='now' THEN NOW() ELSE NULL END)`,
		rideID, partyID, vol, status, matchedAt); err != nil {
		t.Fatalf("seed ride: %v", err)
	}
	return rideID
}

func callUpdateRideStatus(t *testing.T, rideID, status string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"status": status})
	req := httptest.NewRequest("PATCH", "/gotv/rides/"+rideID+"/status", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GOTV-Party-ID", "1")
	req.Header.Set("X-GOTV-User", "w7-coord")
	req = mux.SetURLVars(req, map[string]string{"id": rideID})
	rr := httptest.NewRecorder()
	handleUpdateRideStatus(rr, req)
	return rr
}

func TestRideNoShowReturnsToPending(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}
	rideID := seedRideFixture(t, "matched")

	rr := callUpdateRideStatus(t, rideID, "no_show")
	if rr.Code != 200 {
		t.Fatalf("no_show update returned %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["rematch"] != "pending" {
		t.Fatalf("expected rematch=pending, got %v", resp)
	}

	var status string
	var volunteer *string
	if err := dbConn.QueryRow(`SELECT status, volunteer_id FROM gotv_ride_requests WHERE request_id=$1`, rideID).
		Scan(&status, &volunteer); err != nil {
		t.Fatalf("read ride: %v", err)
	}
	if status != "pending" && status != "matched" {
		t.Fatalf("ride after no_show must be pending (or async-rematched), got %q", status)
	}
	if status == "pending" && volunteer != nil {
		t.Fatalf("pending ride must have no dead volunteer_id, got %v", *volunteer)
	}
}

func TestRideDriverCancelReturnsToPending(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}
	rideID := seedRideFixture(t, "matched")

	rr := callUpdateRideStatus(t, rideID, "cancelled")
	if rr.Code != 200 {
		t.Fatalf("cancelled update returned %d: %s", rr.Code, rr.Body.String())
	}
	var status string
	if err := dbConn.QueryRow(`SELECT status FROM gotv_ride_requests WHERE request_id=$1`, rideID).Scan(&status); err != nil {
		t.Fatalf("read ride: %v", err)
	}
	if status != "pending" && status != "matched" {
		t.Fatalf("matched ride cancelled by driver must return to pending, got %q", status)
	}
}

func TestRideVoterCancelStaysCancelled(t *testing.T) {
	if dbConn == nil {
		t.Skip("requires database")
	}
	// A still-pending ride (no live assignment) cancelled by the voter is a
	// true cancellation — no re-match.
	rideID := seedRideFixture(t, "pending")

	rr := callUpdateRideStatus(t, rideID, "cancelled")
	if rr.Code != 200 {
		t.Fatalf("cancel pending ride returned %d: %s", rr.Code, rr.Body.String())
	}
	var status string
	if err := dbConn.QueryRow(`SELECT status FROM gotv_ride_requests WHERE request_id=$1`, rideID).Scan(&status); err != nil {
		t.Fatalf("read ride: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("voter-cancelled pending ride must stay cancelled, got %q", status)
	}
}
